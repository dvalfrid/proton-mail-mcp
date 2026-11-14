package setup

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"runtime"

	"github.com/dvalfrid/proton-mail-mcp/internal/bridge"
	"github.com/dvalfrid/proton-mail-mcp/internal/config"
	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
)

// The steps below are what Run does, one at a time, for a front end that
// isn't a terminal (the setup wizard). Each returns what it found and leaves
// the wording to the caller. They write nothing except Save.

// ErrStore means the password could not be put in the OS credential store.
var ErrStore = errors.New("the password could not be stored in the OS credential store")

// Prepare fills in defaults, and the ports from an earlier setup, and returns
// that earlier setup's settings (zero if there was none).
func (o *Options) Prepare() (config.Settings, error) {
	o.defaults()
	prev, err := config.ReadSettings(config.SettingsFile(o.Dir))
	if err != nil {
		return config.Settings{}, err
	}
	if o.IMAPPort == 0 {
		o.IMAPPort = cmpOr(prev.IMAPPort, config.DefaultIMAPPort)
	}
	if o.SMTPPort == 0 {
		o.SMTPPort = cmpOr(prev.SMTPPort, config.DefaultSMTPPort)
	}
	return prev, nil
}

// BridgeStatus is what LookForBridge found on the IMAP port.
type BridgeStatus struct {
	Addr        string
	Answers     bool   // something answers on Addr
	IsBridge    bool   // and it greets like Bridge
	Installed   bool   // when nothing answers: Bridge is installed
	InstalledAt string // where, if known
}

// LookForBridge checks once whether Bridge answers on the IMAP port.
func (o *Options) LookForBridge(ctx context.Context) BridgeStatus {
	s := BridgeStatus{Addr: addr(o.Host, o.IMAPPort)}
	greeting, err := bridge.IMAPGreeting(ctx, s.Addr)
	if err == nil {
		s.Answers = true
		s.IsBridge = bridge.IsBridgeGreeting(greeting)
		return s
	}
	s.InstalledAt, s.Installed = o.Installed()
	return s
}

// Certificate is Bridge's certificate as read from the IMAP port, not yet
// trusted, with what is known about the program that holds the port.
type Certificate struct {
	cert            *x509.Certificate
	Fingerprint     string
	LooksLikeBridge bool   // issued the way Bridge issues its own
	SMTPProblem     string // why SMTP could not be checked; reading mail works without it
	OwnerKnown      bool   // the program on the port could be identified
	Owner           bridge.Owner
	// Verified: the port is held by a signed bridge.exe in Program Files and
	// the certificate looks like Bridge's, so it may be pinned without
	// asking. Otherwise the user must confirm (issue #71).
	Verified bool
}

// PEM is the certificate as written to disk.
func (c Certificate) PEM() []byte {
	if c.cert == nil {
		return nil
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.cert.Raw})
}

// ReadCertificate reads Bridge's certificate without trusting it, checks
// that SMTP presents the same one, and identifies the program on the port.
// It fails with bridge.ErrNotBridge when SMTP shows a different certificate.
func (o *Options) ReadCertificate(ctx context.Context) (Certificate, error) {
	cert, err := bridge.IMAPCertificate(ctx, addr(o.Host, o.IMAPPort), o.Host)
	if err != nil {
		return Certificate{}, fmt.Errorf("read Bridge's certificate: %w", err)
	}
	c := Certificate{cert: cert, Fingerprint: bridge.Fingerprint(cert), LooksLikeBridge: bridge.LooksLikeBridge(cert)}
	if smtp, err := bridge.SMTPCertificate(ctx, addr(o.Host, o.SMTPPort), o.Host); err != nil {
		c.SMTPProblem = fmt.Sprintf("SMTP on %s didn't answer as expected (%v)", addr(o.Host, o.SMTPPort), err)
	} else if bridge.Fingerprint(smtp) != c.Fingerprint {
		return Certificate{}, fmt.Errorf("SMTP on %s presents a different certificate than IMAP: something other than Bridge is on one of the ports: %w", addr(o.Host, o.SMTPPort), bridge.ErrNotBridge)
	}
	if owner, err := o.PortOwner(o.IMAPPort); err == nil {
		c.OwnerKnown, c.Owner = true, owner
		// Only a signed bridge.exe in Program Files is trusted without
		// asking: a signature alone can be borrowed (issue #71).
		c.Verified = owner.TrustedBridge() && c.LooksLikeBridge
	}
	return c, nil
}

// CanVerifyOwner reports whether this platform can identify the program on
// Bridge's port; elsewhere the user always confirms the certificate.
func CanVerifyOwner() bool { return runtime.GOOS == "windows" }

// TestLogin logs in to Bridge with the given username and password, trusting
// only the certificate c. It writes nothing.
func (o *Options) TestLogin(ctx context.Context, user, password string, c Certificate) error {
	tlsCfg, err := mail.TLSConfigFromPEM(o.Host, c.PEM())
	if err != nil {
		return err
	}
	m := mail.New(mail.Options{Addr: addr(o.Host, o.IMAPPort), User: user, Password: config.Secret(password), TLSConfig: tlsCfg, Timeout: bridge.Timeout, Logger: o.Logger})
	err = m.Ping(ctx)
	_ = m.Close() // best effort
	return err
}

// Save writes the certificate and settings, and the password to the OS
// credential store, and returns the settings file. Call it only after
// TestLogin has worked. A failing credential store is ErrStore.
func (o *Options) Save(user, password string, c Certificate) (string, error) {
	certFile := config.CertPath(o.Dir)
	if err := config.WriteFile(certFile, c.PEM()); err != nil {
		return "", err
	}
	file := config.SettingsFile(o.Dir)
	prev, _ := config.ReadSettings(file) // an unreadable file is replaced, without options
	settings := config.Settings{User: user, Host: o.Host, IMAPPort: o.IMAPPort, SMTPPort: o.SMTPPort, CertFile: certFile, Fingerprint: c.Fingerprint}.KeepOptions(prev)
	if err := config.WriteSettings(file, settings); err != nil {
		return "", err
	}
	if err := o.Store.Set(user, password); err != nil {
		return file, fmt.Errorf("%w: %w", ErrStore, err)
	}
	return file, nil
}
