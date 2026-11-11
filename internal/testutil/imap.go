// Package testutil starts in-memory mail servers for tests, so no test needs
// Proton or Bridge. The servers speak real IMAP (go-imap's imapserver with
// the imapmemserver backend) over STARTTLS, like Bridge does.
package testutil

import (
	"bytes"
	"context"
	"crypto/tls"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

// Test account. The password is deliberately distinctive so tests can assert
// it never leaks into output or logs.
const (
	User     = "alice@example.com"
	Password = "test-bridge-password-6f1c"
)

// Folders created in every test server, named like Proton Mail Bridge's.
var Folders = []string{"INBOX", "Drafts", "Sent", "Trash", "Spam", "Archive", "Folders/Work"}

// IMAPServer is a running in-memory IMAP server.
type IMAPServer struct {
	Host string
	Port int

	user       *imapmemserver.User
	cert       *Cert
	transcript *syncBuffer
}

// NewIMAPServer starts a server on 127.0.0.1 with the test account and
// Folders. It is stopped when the test ends.
func NewIMAPServer(t testing.TB) *IMAPServer {
	t.Helper()

	cert := NewCert(t)
	mem := imapmemserver.New()
	user := imapmemserver.NewUser(User, Password)
	for _, name := range Folders {
		if err := user.Create(name, nil); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	mem.AddUser(user)

	transcript := &syncBuffer{}
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		TLSConfig:   &tls.Config{Certificates: []tls.Certificate{cert.TLS}, MinVersion: tls.VersionTLS12},
		Caps:        imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapMove: {}, imap.CapUIDPlus: {}},
		DebugWriter: transcript,
		Logger:      testLogger{t},
	})

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(ln) }() // returns when the server is closed
	t.Cleanup(func() { _ = srv.Close() })

	return &IMAPServer{
		Host:       "127.0.0.1",
		Port:       ln.Addr().(*net.TCPAddr).Port,
		user:       user,
		cert:       cert,
		transcript: transcript,
	}
}

// Addr returns host:port.
func (s *IMAPServer) Addr() string { return net.JoinHostPort(s.Host, strconv.Itoa(s.Port)) }

// CertFile writes the server's certificate to a temp file and returns its
// path, like a certificate exported from Bridge.
func (s *IMAPServer) CertFile(t testing.TB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bridge.pem")
	if err := os.WriteFile(path, s.cert.PEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// AddMessage appends a raw RFC 5322 message to mailbox with the given flags.
// Line endings are normalised to CRLF.
func (s *IMAPServer) AddMessage(t testing.TB, mailbox, raw string, flags ...imap.Flag) {
	t.Helper()
	raw = strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\n", "\r\n")
	if _, err := s.user.Append(mailbox, literal{bytes.NewReader([]byte(raw))}, &imap.AppendOptions{Flags: flags}); err != nil {
		t.Fatalf("append to %s: %v", mailbox, err)
	}
}

// Unseen returns the number of messages without \Seen in mailbox, read
// directly from the backend.
func (s *IMAPServer) Unseen(t testing.TB, mailbox string) uint32 {
	t.Helper()
	st, err := s.user.Status(mailbox, &imap.StatusOptions{NumUnseen: true})
	if err != nil {
		t.Fatalf("status %s: %v", mailbox, err)
	}
	return *st.NumUnseen
}

// Transcript returns everything the server sent and received so far, for
// asserting which commands a client used.
func (s *IMAPServer) Transcript() string { return s.transcript.String() }

type literal struct{ *bytes.Reader }

func (l literal) Size() int64 { return l.Reader.Size() }

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type testLogger struct{ t testing.TB }

func (l testLogger) Printf(format string, args ...any) { l.t.Logf("imapserver: "+format, args...) }
