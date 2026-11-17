package testutil

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"sync"
	"testing"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

// SMTPServer imitates Bridge's SMTP server for send tests: STARTTLS with
// its own certificate, AUTH PLAIN with User and Password only after TLS,
// and it records every message instead of sending it.
type SMTPServer struct {
	Addr string
	cert *Cert

	mu   sync.Mutex
	sent []SMTPMessage
	// Refuse, when set, is the error DATA returns.
	refuse error
}

// SMTPMessage is one message the test server accepted.
type SMTPMessage struct {
	From       string
	Recipients []string
	Data       []byte
	TLS        bool // the session was encrypted when AUTH ran
}

// NewSMTPServer starts a test SMTP server on 127.0.0.1.
func NewSMTPServer(t testing.TB) *SMTPServer {
	t.Helper()
	s := &SMTPServer{cert: NewCert(t)}
	srv := smtp.NewServer(smtp.BackendFunc(func(c *smtp.Conn) (smtp.Session, error) {
		return &smtpSession{s: s, conn: c}, nil
	}))
	srv.Domain = "127.0.0.1"
	srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{s.cert.TLS}, MinVersion: tls.VersionTLS12}
	srv.AllowInsecureAuth = false // AUTH only after STARTTLS
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.Addr = ln.Addr().String()
	done := make(chan struct{})
	go func() { _ = srv.Serve(ln); close(done) }() // ends when closed below
	t.Cleanup(func() { _ = srv.Close(); <-done })
	return s
}

// CertPEM is the server's certificate, as setup pins Bridge's.
func (s *SMTPServer) CertPEM() []byte { return s.cert.PEM }

// Sent returns the messages accepted so far.
func (s *SMTPServer) Sent() []SMTPMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]SMTPMessage(nil), s.sent...)
}

// RefuseData makes DATA fail with err from now on.
func (s *SMTPServer) RefuseData(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refuse = err
}

type smtpSession struct {
	s      *SMTPServer
	conn   *smtp.Conn
	authed bool
	tls    bool
	msg    SMTPMessage
}

func (ss *smtpSession) AuthMechanisms() []string { return []string{sasl.Plain} }

func (ss *smtpSession) Auth(string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(_, user, pass string) error {
		if user != User || pass != Password {
			return errors.New("invalid credentials")
		}
		_, ss.tls = ss.conn.TLSConnectionState()
		ss.authed = true
		return nil
	}), nil
}

func (ss *smtpSession) Mail(from string, _ *smtp.MailOptions) error {
	if !ss.authed {
		return smtp.ErrAuthRequired
	}
	ss.msg = SMTPMessage{From: from, TLS: ss.tls}
	return nil
}

func (ss *smtpSession) Rcpt(to string, _ *smtp.RcptOptions) error {
	ss.msg.Recipients = append(ss.msg.Recipients, to)
	return nil
}

func (ss *smtpSession) Data(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	ss.s.mu.Lock()
	defer ss.s.mu.Unlock()
	if ss.s.refuse != nil {
		return ss.s.refuse
	}
	ss.msg.Data = data
	ss.s.sent = append(ss.s.sent, ss.msg)
	return nil
}

func (ss *smtpSession) Reset()        { ss.msg = SMTPMessage{} }
func (ss *smtpSession) Logout() error { return nil }
