package bridge

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/dvalfrid/proton-mail-mcp/internal/testutil"
)

func pinned(t *testing.T, srv *testutil.IMAPServer) *x509.Certificate {
	t.Helper()
	data, err := os.ReadFile(srv.CertFile(t))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(data)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func TestIMAPGreeting(t *testing.T) {
	t.Parallel()
	b := testutil.NewBridge(t)
	ctx := context.Background()

	g, err := IMAPGreeting(ctx, b.IMAPAddr)
	if err != nil || !IsBridgeGreeting(g) {
		t.Errorf("Bridge: greeting %q, err %v: want a Bridge greeting", g, err)
	}
	g, err = IMAPGreeting(ctx, b.Addr()) // the plain in-memory server
	if err != nil || IsBridgeGreeting(g) {
		t.Errorf("other server: greeting %q, err %v: want a non-Bridge greeting", g, err)
	}
	if IsBridgeGreeting("* BYE Proton Mail Bridge") {
		t.Error("a BYE greeting counted as Bridge")
	}
}

func TestNothingListening(t *testing.T) {
	t.Parallel()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // now nothing listens on addr
	if _, err := IMAPGreeting(context.Background(), addr); err == nil {
		t.Fatal("no error with nothing listening")
	}
}

// The certificate is read without trusting it, and is the one the server
// presents: the same as the pinned file, on IMAP and SMTP alike.
func TestCertificates(t *testing.T) {
	t.Parallel()
	b := testutil.NewBridge(t)
	want := pinned(t, b.IMAPServer)
	ctx := context.Background()

	imapCert, err := IMAPCertificate(ctx, b.IMAPAddr, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	smtpCert, err := SMTPCertificate(ctx, b.SMTPAddr, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if Fingerprint(imapCert) != Fingerprint(want) || Fingerprint(smtpCert) != Fingerprint(want) {
		t.Errorf("fingerprints imap %s, smtp %s, want %s", Fingerprint(imapCert), Fingerprint(smtpCert), Fingerprint(want))
	}
	if !LooksLikeBridge(imapCert) {
		t.Errorf("Bridge-like certificate not recognised: %s", imapCert.Subject)
	}
	if fp := Fingerprint(want); len(fp) != 95 || strings.Count(fp, ":") != 31 {
		t.Errorf("fingerprint format %q", fp)
	}
}

// Guardrail: probing never sends credentials. Only STARTTLS goes over the
// wire before the handshake, and nothing after it.
func TestProbeSendsNoCredentials(t *testing.T) {
	t.Parallel()
	b := testutil.NewBridge(t)
	if _, err := IMAPCertificate(context.Background(), b.IMAPAddr, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	// The server's capability list mentions LOGINDISABLED, so look for
	// commands and credentials, not the bare word.
	tr := b.Transcript()
	for _, s := range []string{" LOGIN ", " AUTHENTICATE ", testutil.Password, testutil.User} {
		if strings.Contains(tr, s) {
			t.Errorf("transcript contains %q", s)
		}
	}
}

func TestNotBridgeGreeting(t *testing.T) {
	t.Parallel()
	addr := fakeServer(t, "220 smtp.example.org ESMTP\r\n")
	if _, err := IMAPCertificate(context.Background(), addr, "127.0.0.1"); !errors.Is(err, ErrNotBridge) {
		t.Errorf("err = %v, want ErrNotBridge", err)
	}
}

// fakeServer writes greeting to every connection and then closes it.
func fakeServer(t *testing.T, greeting string) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() }) // ends the accept loop
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = c.Write([]byte(greeting)) // test server: errors don't matter
			_ = c.Close()
		}
	}()
	return ln.Addr().String()
}
