package send

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/emersion/go-smtp"

	"github.com/dvalfrid/proton-mail-mcp/internal/config"
	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
	"github.com/dvalfrid/proton-mail-mcp/internal/testutil"
)

// draft is a draft as Bridge stores it: its own headers and a References
// entry that names the draft itself (docs/bridge.md §7).
const draft = "From: Owner <owner@example.com>\r\n" +
	"To: Alice <alice@example.com>\r\n" +
	"Cc: bob@example.org\r\n" +
	"Bcc: carol@example.net\r\n" +
	"Subject: Lunch\r\n" +
	"References: <parent@example.com> <abc123@protonmail.internalid>\r\n" +
	"X-Pm-Gluon-Id: 7\r\n" +
	"X-Pm-Internal-Id: abc123\r\n" +
	"X-Pm-External-Id: <x@example.com>\r\n" +
	"X-Pm-Date: Mon, 16 Nov 2026 10:00:00 +0000\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n" +
	"\r\n" +
	"See you at noon.\r\n"

var everyone = []string{"alice@example.com", "bob@example.org", "carol@example.net"}

func TestPrepare(t *testing.T) {
	t.Parallel()
	m, err := Prepare([]byte(draft), []string{"Carol@Example.net", "alice@example.com", "bob@example.org", "bob@example.org"})
	if err != nil {
		t.Fatal(err)
	}
	if m.From != "owner@example.com" || m.Subject != "Lunch" || len(m.Recipients) != 3 {
		t.Errorf("Prepare = %+v", m)
	}
	out := string(m.data)
	for _, h := range internalHeaders {
		if strings.Contains(out, h+":") {
			t.Errorf("%s was not removed:\n%s", h, out)
		}
	}
	for _, keep := range []string{"References: <parent@example.com> <abc123@protonmail.internalid>", "Subject: Lunch", "\r\n\r\nSee you at noon.\r\n"} {
		if !strings.Contains(out, keep) {
			t.Errorf("%q missing:\n%s", keep, out)
		}
	}
}

// The wrong draft, or a recipient added or left out, sends nothing.
func TestPrepareRefuses(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		raw  string
		to   []string
		want error
	}{
		"one missing":   {draft, everyone[:2], ErrRecipients},
		"one extra":     {draft, append([]string{"eve@example.com"}, everyone...), ErrRecipients},
		"none given":    {draft, nil, ErrRecipients},
		"no recipients": {"From: owner@example.com\r\nSubject: x\r\n\r\nbody", []string{"a@example.com"}, ErrNotSendable},
		"two senders":   {"From: a@example.com, b@example.com\r\nTo: c@example.com\r\n\r\nbody", []string{"c@example.com"}, ErrNotSendable},
		"no header end": {"From: owner@example.com", []string{"a@example.com"}, ErrNotSendable},
	} {
		if _, err := Prepare([]byte(c.raw), c.to); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
	_, err := Prepare([]byte(draft), everyone[:1])
	if err == nil || !strings.Contains(err.Error(), "bob@example.org") {
		t.Errorf("the refusal doesn't name the draft's recipients: %v", err)
	}
}

func sender(t *testing.T, srv *testutil.SMTPServer, allowed bool) *Sender {
	t.Helper()
	tlsCfg, err := mail.TLSConfigFromPEM("127.0.0.1", srv.CertPEM())
	if err != nil {
		t.Fatal(err)
	}
	return New(Options{Allowed: allowed, Addr: srv.Addr, TLSConfig: tlsCfg, User: testutil.User, Password: config.Secret(testutil.Password)})
}

func TestSend(t *testing.T) {
	t.Parallel()
	srv := testutil.NewSMTPServer(t)
	m, err := Prepare([]byte(draft), everyone)
	if err != nil {
		t.Fatal(err)
	}
	if err := sender(t, srv, true).Send(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	sent := srv.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages", len(sent))
	}
	got := sent[0]
	if !got.TLS {
		t.Error("AUTH ran without TLS")
	}
	// Every To, Cc and Bcc address goes in RCPT TO: Bridge drops the rest.
	if got.From != "owner@example.com" || strings.Join(got.Recipients, ",") != strings.Join(everyone, ",") {
		t.Errorf("envelope = %s -> %v", got.From, got.Recipients)
	}
	if strings.Contains(string(got.Data), "X-Pm-") || !strings.Contains(string(got.Data), "protonmail.internalid") {
		t.Errorf("data:\n%s", got.Data)
	}
}

func TestSendRefuses(t *testing.T) {
	t.Parallel()
	srv := testutil.NewSMTPServer(t)
	m, err := Prepare([]byte(draft), everyone)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	off := sender(t, srv, false)
	if err := off.Check(); !errors.Is(err, ErrOff) {
		t.Errorf("off: Check = %v", err)
	}
	if err := off.Send(ctx, m); !errors.Is(err, ErrOff) {
		t.Errorf("off: Send = %v", err)
	}
	var none *Sender
	if err := none.Check(); !errors.Is(err, ErrOff) {
		t.Errorf("nil: Check = %v", err)
	}

	// A certificate other than the pinned one: nothing is sent, and the
	// password never leaves.
	other := testutil.NewSMTPServer(t)
	s := sender(t, srv, true)
	s.opts.Addr = other.Addr
	if err := s.Send(ctx, m); err == nil || len(other.Sent()) != 0 {
		t.Errorf("unpinned certificate: err = %v, sent %d", err, len(other.Sent()))
	}

	s = sender(t, srv, true)
	s.opts.Addr = "192.0.2.1:1025"
	if err := s.Send(ctx, m); !errors.Is(err, ErrNotLocal) {
		t.Errorf("not local: %v", err)
	}

	srv.RefuseData(&smtp.SMTPError{Code: 554, Message: "refused"})
	if err := sender(t, srv, true).Send(ctx, m); err == nil {
		t.Error("a refused DATA reported success")
	}
	if len(srv.Sent()) != 0 {
		t.Errorf("sent %d", len(srv.Sent()))
	}
}

// At most MaxSendsPerRun attempts per run, counted whether or not they
// succeed.
func TestSendLimit(t *testing.T) {
	t.Parallel()
	srv := testutil.NewSMTPServer(t)
	m, err := Prepare([]byte(draft), everyone)
	if err != nil {
		t.Fatal(err)
	}
	s := sender(t, srv, true)
	for i := range MaxSendsPerRun {
		if err := s.Send(context.Background(), m); err != nil {
			t.Fatalf("send %d: %v", i+1, err)
		}
	}
	if err := s.Check(); !errors.Is(err, ErrLimit) {
		t.Errorf("Check after the limit = %v", err)
	}
	if err := s.Send(context.Background(), m); !errors.Is(err, ErrLimit) {
		t.Errorf("Send after the limit = %v", err)
	}
	if n := len(srv.Sent()); n != MaxSendsPerRun {
		t.Errorf("sent %d, want %d", n, MaxSendsPerRun)
	}
}
