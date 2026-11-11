package testutil

import (
	"bufio"
	"context"
	"crypto/tls"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
)

// BridgeGreeting is the IMAP greeting a real Bridge sends.
const BridgeGreeting = "* OK [CAPABILITY AUTH=PLAIN ID IDLE IMAP4rev1 MOVE STARTTLS UIDPLUS UNSELECT] Proton Mail Bridge 03.27.01 - gluon session ID 1"

// Bridge imitates Proton Mail Bridge on the wire, for the setup and check
// commands: IMAP with Bridge's greeting in front of an in-memory server, and
// SMTP that offers STARTTLS with the same certificate.
type Bridge struct {
	*IMAPServer
	IMAPAddr string // with Bridge's greeting
	SMTPAddr string
}

// NewBridge starts an in-memory IMAP server, a proxy that replaces its
// greeting with Bridge's, and a minimal SMTP server.
func NewBridge(t testing.TB) *Bridge {
	t.Helper()
	b := &Bridge{IMAPServer: NewIMAPServer(t)}
	b.IMAPAddr = serve(t, func(c net.Conn) { greetingProxy(c, b.Addr()) })
	b.SMTPAddr = serve(t, func(c net.Conn) { smtpStartTLS(c, b.cert.TLS) })
	return b
}

// serve listens on 127.0.0.1 and handles each connection on its own
// goroutine until the test ends.
func serve(t testing.TB, handle func(net.Conn)) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	t.Cleanup(func() {
		_ = ln.Close() // stops the accept loop
		wg.Wait()
	})
	wg.Go(func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Go(func() {
				defer func() { _ = c.Close() }() // test connection
				handle(c)
			})
		}
	})
	return ln.Addr().String()
}

// greetingProxy forwards a connection to backend, replacing backend's first
// line with Bridge's greeting. After that it copies bytes both ways, so
// STARTTLS and TLS pass through unchanged.
func greetingProxy(client net.Conn, backend string) {
	server, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", backend)
	if err != nil {
		return
	}
	defer func() { _ = server.Close() }() // test connection
	r := bufio.NewReader(server)
	if _, err := r.ReadString('\n'); err != nil {
		return
	}
	if _, err := io.WriteString(client, BridgeGreeting+"\r\n"); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(server, client); done <- struct{}{} }() // ends when either side closes
	go func() { _, _ = io.Copy(client, r); done <- struct{}{} }()
	<-done
}

// smtpStartTLS answers EHLO and STARTTLS like Bridge's SMTP server, then
// completes the TLS handshake and closes.
func smtpStartTLS(c net.Conn, cert tls.Certificate) {
	r := bufio.NewReader(c)
	if _, err := io.WriteString(c, "220 127.0.0.1 ESMTP Service Ready\r\n"); err != nil {
		return
	}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
		case strings.HasPrefix(cmd, "EHLO"):
			_, _ = io.WriteString(c, "250-127.0.0.1\r\n250-STARTTLS\r\n250 AUTH PLAIN LOGIN\r\n") // a failure ends the next read
		case cmd == "STARTTLS":
			if _, err := io.WriteString(c, "220 Ready to start TLS\r\n"); err != nil {
				return
			}
			tc := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
			_ = tc.HandshakeContext(context.Background()) // the client may abort after reading the certificate
			return
		default:
			_, _ = io.WriteString(c, "502 Not implemented\r\n") // a failure ends the next read
		}
	}
}
