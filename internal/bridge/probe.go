// Package bridge recognises Proton Mail Bridge on localhost and reads its
// TLS certificate, for the setup and check commands. It never logs in and
// never sends credentials: everything here happens before authentication.
//
// The certificate is read without switching verification off. The TLS
// handshake is made against an empty set of trusted roots, so verification
// fails, and the certificate the server presented is taken from Go's
// verification error. Nothing is ever sent over that unverified connection.
package bridge

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"
)

// Timeout bounds each probe. Bridge on localhost answers at once.
const Timeout = 10 * time.Second

// Greeting text Bridge sends before STARTTLS: "* OK [CAPABILITY …] Proton
// Mail Bridge 03.27.01 - gluon session ID 6" (docs/installation-research.md
// §B1). It is plain text and anyone can send it: it is used for clear error
// messages, never for trust.
const greetingName = "Proton Mail Bridge"

// ErrNotBridge means something answered on the port, but not as Bridge.
var ErrNotBridge = errors.New("the server on this port does not identify itself as Proton Mail Bridge")

// IMAPGreeting connects to addr and returns the IMAP greeting line.
func IMAPGreeting(ctx context.Context, addr string) (string, error) {
	conn, r, err := dial(ctx, addr)
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }() // read-only probe
	return readLine(r)
}

// IsBridgeGreeting reports whether an IMAP greeting names Proton Mail Bridge.
func IsBridgeGreeting(greeting string) bool {
	return strings.HasPrefix(greeting, "* OK") && strings.Contains(greeting, greetingName)
}

// IMAPCertificate returns the certificate the IMAP server at addr presents
// after STARTTLS, without trusting it. host is the TLS server name.
func IMAPCertificate(ctx context.Context, addr, host string) (*x509.Certificate, error) {
	conn, r, err := dial(ctx, addr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }() // nothing is sent after the handshake

	greeting, err := readLine(r)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(greeting, "* OK") {
		return nil, fmt.Errorf("unexpected IMAP greeting: %w", ErrNotBridge)
	}
	if _, err := conn.Write([]byte("a1 STARTTLS\r\n")); err != nil {
		return nil, fmt.Errorf("send STARTTLS: %w", err)
	}
	for {
		line, err := readLine(r)
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(line, "a1 ") {
			if !strings.HasPrefix(line, "a1 OK") {
				return nil, fmt.Errorf("STARTTLS refused: %s", clip(line))
			}
			break
		}
	}
	return peerCertificate(ctx, conn, host)
}

// SMTPCertificate returns the certificate the SMTP server at addr presents
// after STARTTLS, without trusting it.
func SMTPCertificate(ctx context.Context, addr, host string) (*x509.Certificate, error) {
	conn, r, err := dial(ctx, addr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }() // nothing is sent after the handshake

	if err := smtpReply(r, "220"); err != nil {
		return nil, fmt.Errorf("SMTP greeting: %w", err)
	}
	if _, err := conn.Write([]byte("EHLO localhost\r\n")); err != nil {
		return nil, fmt.Errorf("send EHLO: %w", err)
	}
	if err := smtpReply(r, "250"); err != nil {
		return nil, fmt.Errorf("EHLO: %w", err)
	}
	if _, err := conn.Write([]byte("STARTTLS\r\n")); err != nil {
		return nil, fmt.Errorf("send STARTTLS: %w", err)
	}
	if err := smtpReply(r, "220"); err != nil {
		return nil, fmt.Errorf("STARTTLS: %w", err)
	}
	return peerCertificate(ctx, conn, host)
}

// Fingerprint is the SHA-256 of a certificate, as colon-separated hex, the
// way certificate viewers show it.
func Fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":")
}

// LooksLikeBridge reports whether a certificate has the shape of the one
// Bridge generates: self-signed, issued to Proton AG / Proton Mail for
// 127.0.0.1 (docs/installation-research.md §B2). Anyone can make such a
// certificate: this is a sanity check, not proof.
func LooksLikeBridge(cert *x509.Certificate) bool {
	s := cert.Subject
	return cert.Issuer.String() == s.String() &&
		slices.Contains(s.Organization, "Proton AG") &&
		slices.Contains(s.OrganizationalUnit, "Proton Mail") &&
		s.CommonName == "127.0.0.1"
}

func dial(ctx context.Context, addr string) (net.Conn, *bufio.Reader, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to %s: %w", addr, err)
	}
	if err := conn.SetDeadline(time.Now().Add(Timeout)); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("set deadline: %w", err)
	}
	return conn, bufio.NewReader(conn), nil
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read from server: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// smtpReply reads a possibly multi-line SMTP reply and checks its code.
func smtpReply(r *bufio.Reader, code string) error {
	for {
		line, err := readLine(r)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(line, code) {
			return fmt.Errorf("unexpected reply: %s", clip(line))
		}
		if len(line) < 4 || line[3] != '-' {
			return nil
		}
	}
}

// peerCertificate runs the TLS handshake against an empty root pool, so it
// always fails verification, and returns the certificate the server sent.
// The connection is never used after this.
func peerCertificate(ctx context.Context, conn net.Conn, host string) (*x509.Certificate, error) {
	tc := tls.Client(conn, &tls.Config{ServerName: host, RootCAs: x509.NewCertPool(), MinVersion: tls.VersionTLS12})
	err := tc.HandshakeContext(ctx)
	var verr *tls.CertificateVerificationError
	switch {
	case errors.As(err, &verr) && len(verr.UnverifiedCertificates) > 0:
		return verr.UnverifiedCertificates[0], nil
	case err != nil:
		return nil, fmt.Errorf("TLS handshake: %w", err)
	default:
		// Unreachable with an empty root pool; refuse rather than guess.
		return nil, errors.New("TLS handshake unexpectedly verified against an empty root pool")
	}
}

// clip keeps server text in errors short: it is not ours.
func clip(s string) string {
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}
