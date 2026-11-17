// Package send submits an existing draft to Proton Mail Bridge over SMTP,
// for send_draft. It is the only package that talks SMTP (depguard,
// STANDARDS.md#sending): nothing else can send mail.
//
// A send is refused unless the owner switched sending on, at most
// MaxSendsPerRun times per server run, and only when the caller names the
// draft's recipients exactly. The connection goes to Bridge on localhost
// only, with STARTTLS verified against Bridge's pinned certificate before
// the password is sent.
package send

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-message/mail"
	"github.com/emersion/go-message/textproto"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"

	"github.com/dvalfrid/proton-mail-mcp/internal/config"
)

// MaxSendsPerRun caps the sends of one server run, apart from
// PROTON_MAX_CHANGES: a guard against a fooled assistant sending many.
const MaxSendsPerRun = 10

// maxRecipients caps one message's recipients, as the draft tools do for
// each of To, Cc and Bcc.
const maxRecipients = 150

var (
	// ErrOff means the owner hasn't switched sending on.
	ErrOff = errors.New("sending is switched off. The owner switches it on outside Claude: " +
		"on Windows on the Settings page of Proton Mail MCP (Start menu), elsewhere with: proton-mail-mcp settings set allow_send on (or " + config.EnvAllowSend + "=1), then restarts Claude. " +
		"Until then the owner can send the draft from Proton Mail")
	// ErrLimit means this server run has sent MaxSendsPerRun messages.
	ErrLimit = fmt.Errorf("this server run has sent %d messages, its limit; the owner restarts Claude to send more, or sends from Proton Mail", MaxSendsPerRun)
	// ErrRecipients means the recipients given don't match the draft's.
	ErrRecipients = errors.New("the recipients don't match the draft's, so nothing was sent")
	// ErrNotSendable means the draft can't be sent as it is.
	ErrNotSendable = errors.New("the draft can't be sent")
	// ErrNotLocal means the SMTP address isn't Bridge on this computer.
	ErrNotLocal = errors.New("sending goes only to Proton Mail Bridge on this computer (127.0.0.1 or localhost)")
)

// internalHeaders are the headers Bridge adds to a message it stores
// (docs/bridge.md §7, §8). They are Bridge's own, not the owner's, and are
// removed before the draft is submitted. References stays: Bridge finds
// the draft and a reply's parent through it.
var internalHeaders = []string{"X-Pm-Gluon-Id", "X-Pm-Internal-Id", "X-Pm-External-Id", "X-Pm-Date"}

// Message is a draft ready to submit.
type Message struct {
	From       string   // the sender's address
	Recipients []string // To, Cc and Bcc addresses, each once, as written
	Subject    string
	data       []byte
}

// Prepare reads a draft's raw message and checks it against the
// recipients the caller named: the same set as the draft's To, Cc and Bcc,
// ignoring case and order. It removes Bridge's internal headers.
func Prepare(raw []byte, recipients []string) (Message, error) {
	head, body, ok := splitHeader(raw)
	if !ok {
		return Message{}, fmt.Errorf("%w: it has no header", ErrNotSendable)
	}
	h, err := textproto.ReadHeader(bufioReader(head))
	if err != nil {
		return Message{}, fmt.Errorf("%w: its header can't be read", ErrNotSendable)
	}
	mh := mail.Header{Header: messageHeader(h)}
	from, err := mh.AddressList("From")
	if err != nil || len(from) != 1 {
		return Message{}, fmt.Errorf("%w: it must have exactly one From address", ErrNotSendable)
	}
	var have []string
	seen := map[string]bool{}
	for _, field := range []string{"To", "Cc", "Bcc"} {
		list, err := mh.AddressList(field)
		if err != nil {
			return Message{}, fmt.Errorf("%w: its %s can't be read", ErrNotSendable, field)
		}
		for _, a := range list {
			if k := strings.ToLower(a.Address); !seen[k] {
				seen[k] = true
				have = append(have, a.Address)
			}
		}
	}
	switch {
	case len(have) == 0:
		return Message{}, fmt.Errorf("%w: it has no recipients", ErrNotSendable)
	case len(have) > maxRecipients:
		return Message{}, fmt.Errorf("%w: more than %d recipients", ErrNotSendable, maxRecipients)
	}
	if !sameAddresses(have, recipients) {
		return Message{}, fmt.Errorf("%w. The draft goes to: %s", ErrRecipients, strings.Join(have, ", "))
	}
	subject, _ := mh.Subject() // a subject that can't be decoded is shown raw
	if subject == "" {
		subject = h.Get("Subject")
	}

	for _, k := range internalHeaders {
		h.Del(k)
	}
	var out bytes.Buffer
	if err := textproto.WriteHeader(&out, h); err != nil {
		return Message{}, fmt.Errorf("%w: %w", ErrNotSendable, err)
	}
	out.Write(body)
	return Message{From: from[0].Address, Recipients: have, Subject: subject, data: out.Bytes()}, nil
}

// sameAddresses reports whether a and b hold the same addresses, ignoring
// case, order and repeats.
func sameAddresses(a, b []string) bool {
	set := func(l []string) []string {
		var out []string
		for _, s := range l {
			out = append(out, strings.ToLower(strings.TrimSpace(s)))
		}
		slices.Sort(out)
		return slices.Compact(out)
	}
	return slices.Equal(set(a), set(b))
}

// Options are how to reach Bridge's SMTP server.
type Options struct {
	Allowed   bool        // the owner switched sending on
	Addr      string      // Bridge's SMTP address: 127.0.0.1 or localhost only
	TLSConfig *tls.Config // verifies Bridge's pinned certificate
	User      string
	Password  config.Secret
	Timeout   time.Duration // for the whole send
}

// Sender sends drafts. It counts its sends for MaxSendsPerRun.
type Sender struct {
	opts Options
	mu   sync.Mutex
	sent int
}

// New returns a Sender with opts.
func New(opts Options) *Sender {
	if opts.Timeout == 0 {
		opts.Timeout = 60 * time.Second
	}
	return &Sender{opts: opts}
}

// Check says whether a send may start: sending is on and the limit not
// reached. It reserves nothing; Send checks again.
func (s *Sender) Check() error {
	if s == nil || !s.opts.Allowed {
		return ErrOff
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sent >= MaxSendsPerRun {
		return ErrLimit
	}
	return nil
}

// Send submits m to Bridge. A send counts against MaxSendsPerRun when it is
// attempted, whether or not Bridge accepts it. It never retries: Bridge
// treats the same message within 90 seconds as already sent.
func (s *Sender) Send(ctx context.Context, m Message) error {
	if err := s.reserve(); err != nil {
		return err
	}
	if !local(s.opts.Addr) {
		return ErrNotLocal
	}
	ctx, cancel := context.WithTimeout(ctx, s.opts.Timeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", s.opts.Addr)
	if err != nil {
		return fmt.Errorf("connect to Bridge's SMTP server at %s: %w", s.opts.Addr, err)
	}
	defer func() { _ = conn.Close() }() // the result of the send is what matters
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl) // a conn that can't take a deadline still ends with ctx
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	// STARTTLS first, verified, before AUTH: Bridge would accept AUTH in
	// clear text (docs/bridge.md §8), we never send it.
	c, err := smtp.NewClientStartTLS(conn, s.opts.TLSConfig)
	if err != nil {
		return fmt.Errorf("start TLS with Bridge's SMTP server: %w", err)
	}
	defer func() { _ = c.Close() }() // after Quit, or on failure
	if err := c.Auth(sasl.NewPlainClient("", s.opts.User, s.opts.Password.Reveal())); err != nil {
		return fmt.Errorf("log in to Bridge's SMTP server: %w", err)
	}
	if err := c.Mail(m.From, nil); err != nil {
		return fmt.Errorf("the sender %s was refused by Bridge: %w", m.From, err)
	}
	for _, r := range m.Recipients {
		if err := c.Rcpt(r, nil); err != nil {
			return fmt.Errorf("the recipient %s was refused by Bridge: %w", r, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("send the message: %w", err)
	}
	if _, err := w.Write(m.data); err != nil {
		_ = w.Close() // the write error is the one to report
		return fmt.Errorf("send the message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("the message was not sent by Bridge: %w", err)
	}
	_ = c.Quit() // sent; a failed goodbye changes nothing
	return nil
}

func (s *Sender) reserve() error {
	if s == nil || !s.opts.Allowed {
		return ErrOff
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sent >= MaxSendsPerRun {
		return ErrLimit
	}
	s.sent++
	return nil
}

// local reports whether addr is on this computer by name or address.
func local(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
