// Package setup is the `proton-mail-mcp setup` command: it finds Proton
// Mail Bridge, pins its certificate, asks for the Bridge password, tests the
// login, stores everything, and offers to register the server in Claude.
//
// The user runs it in a terminal and types the password there: it never
// passes through the AI client. The password goes to the OS credential
// store; the other settings go to a file only the user can read.
package setup

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/dvalfrid/proton-mail-mcp/internal/bridge"
	"github.com/dvalfrid/proton-mail-mcp/internal/config"
	"github.com/dvalfrid/proton-mail-mcp/internal/keystore"
	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
)

// Prompter asks the user. Secret must not echo what is typed.
type Prompter interface {
	Line(prompt, def string) (string, error)
	Secret(prompt string) (string, error)
	Confirm(prompt string, def bool) (bool, error)
}

// Client is an MCP client the server can be registered in.
type Client interface {
	Name() string
	Available() bool  // the client is installed
	Registered() bool // it already has the server
	Register(exe string) error
}

// Options configure a run. Zero ports mean Bridge's defaults.
type Options struct {
	Out      io.Writer
	Prompt   Prompter
	Store    keystore.Store
	Dir      string // where settings and the certificate are written
	Exe      string // this program, as clients should start it
	Host     string
	IMAPPort int
	SMTPPort int
	Clients  []Client
	Logger   *slog.Logger
	// PortOwner is bridge.PortOwner; tests replace it.
	PortOwner func(port int) (bridge.Owner, error)
	// Installed is bridge.Installed; tests replace it.
	Installed func() (string, bool)
}

// ErrAborted means the user chose to stop.
var ErrAborted = errors.New("setup stopped")

const passwordTries = 3

// Run performs the setup. It writes nothing until the login has worked.
func Run(ctx context.Context, o Options) error {
	o.defaults()
	say := func(format string, args ...any) { _, _ = fmt.Fprintf(o.Out, format+"\n", args...) } // a broken terminal can't be reported anywhere

	prev, err := config.ReadSettings(config.SettingsFile(o.Dir))
	if err != nil {
		return err
	}
	if o.IMAPPort == 0 {
		o.IMAPPort = cmpOr(prev.IMAPPort, config.DefaultIMAPPort)
	}
	if o.SMTPPort == 0 {
		o.SMTPPort = cmpOr(prev.SMTPPort, config.DefaultSMTPPort)
	}

	say("Proton Mail Bridge")
	if err := o.findBridge(ctx, say); err != nil {
		return err
	}

	cert, err := o.pinCertificate(ctx, say)
	if err != nil {
		return err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})

	say("")
	say("Bridge account (Bridge → your account → Mailbox details)")
	user, password, err := o.credentials(ctx, say, cmpOr(prev.User, ""), certPEM)
	if err != nil {
		return err
	}

	certFile := config.CertPath(o.Dir)
	if err := config.WriteFile(certFile, certPEM); err != nil {
		return err
	}
	settings := config.Settings{User: user, Host: o.Host, IMAPPort: o.IMAPPort, SMTPPort: o.SMTPPort, CertFile: certFile, Fingerprint: bridge.Fingerprint(cert)}
	if err := config.WriteSettings(config.SettingsFile(o.Dir), settings); err != nil {
		return err
	}
	if err := o.Store.Set(user, password); err != nil {
		say("✗ The password could not be stored in the OS credential store: %v", err)
		say("  Set %s in the environment the MCP client starts the server with, or in a .env file next to the program.", config.EnvPassword)
		return err
	}
	say("✓ Saved: settings in %s, the password in the OS credential store", config.SettingsFile(o.Dir))

	say("")
	say("Claude")
	o.register(say)
	say("")
	say("Done. Run `proton-mail-mcp check` at any time to test the connection.")
	return nil
}

func (o *Options) defaults() {
	if o.Host == "" {
		o.Host = config.DefaultHost
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.PortOwner == nil {
		o.PortOwner = bridge.PortOwner
	}
	if o.Installed == nil {
		o.Installed = bridge.Installed
	}
}

func cmpOr[T comparable](v, def T) T {
	var zero T
	if v == zero {
		return def
	}
	return v
}

// findBridge waits for Bridge to answer on the IMAP port, asking for
// another port if needed, and refuses a server that isn't Bridge.
func (o *Options) findBridge(ctx context.Context, say func(string, ...any)) error {
	for {
		addr := addr(o.Host, o.IMAPPort)
		greeting, err := bridge.IMAPGreeting(ctx, addr)
		if err == nil {
			if !bridge.IsBridgeGreeting(greeting) {
				say("✗ Something answers on %s, but it is not Proton Mail Bridge.", addr)
				say("  Check the IMAP port that Bridge shows, and run setup again with -imap-port.")
				return bridge.ErrNotBridge
			}
			say("✓ Bridge answers on %s", addr)
			return nil
		}
		if where, ok := o.Installed(); ok {
			say("✗ Nothing answers on %s. Bridge is installed (%s) but doesn't seem to be running.", addr, where)
			say("  Start Proton Mail Bridge and sign in.")
		} else {
			say("✗ Nothing answers on %s, and Proton Mail Bridge doesn't seem to be installed.", addr)
			say("  Install it from https://proton.me/mail/bridge, sign in, then run setup again.")
		}
		answer, err := o.Prompt.Line("Press Enter to try again once Bridge runs, type another IMAP port, or q to quit", "")
		if err != nil {
			return err
		}
		switch answer = strings.TrimSpace(answer); {
		case strings.EqualFold(answer, "q"):
			return ErrAborted
		case answer != "":
			p, err := strconv.Atoi(answer)
			if err != nil || p < 1 || p > 65535 {
				say("  %q is not a port number.", answer)
				continue
			}
			o.IMAPPort = p
		}
	}
}

// pinCertificate reads Bridge's certificate without trusting it, checks it,
// and asks the user to confirm unless the Windows port owner check proves
// it is Proton's signed Bridge.
func (o *Options) pinCertificate(ctx context.Context, say func(string, ...any)) (*x509.Certificate, error) {
	cert, err := bridge.IMAPCertificate(ctx, addr(o.Host, o.IMAPPort), o.Host)
	if err != nil {
		return nil, fmt.Errorf("read Bridge's certificate: %w", err)
	}
	fp := bridge.Fingerprint(cert)
	if !bridge.LooksLikeBridge(cert) {
		say("! The certificate is not the kind Bridge makes (issued to %s).", cert.Subject)
	}
	if smtp, err := bridge.SMTPCertificate(ctx, addr(o.Host, o.SMTPPort), o.Host); err != nil {
		say("! SMTP on %s didn't answer as expected (%v). Reading mail works without it.", addr(o.Host, o.SMTPPort), err)
	} else if bridge.Fingerprint(smtp) != fp {
		say("✗ SMTP on %s presents a different certificate than IMAP: something other than Bridge is on one of the ports.", addr(o.Host, o.SMTPPort))
		return nil, bridge.ErrNotBridge
	}
	say("  Certificate SHA-256: %s", fp)

	verified := false
	if owner, err := o.PortOwner(o.IMAPPort); err == nil {
		verified = strings.EqualFold(filepath.Base(owner.Path), "bridge.exe") && owner.Signed && bridge.LooksLikeBridge(cert)
		say("  The port belongs to %s (validly signed: %v)", owner.Path, owner.Signed)
	}
	if verified {
		say("✓ Pinned: the port is held by Proton's signed Bridge")
		return cert, nil
	}
	if runtime.GOOS == "windows" {
		say("! Could not confirm that the program on the port is Proton's signed Bridge.")
	}
	ok, err := o.Prompt.Confirm("Is Proton Mail Bridge running and signed in on this computer, so this is its certificate?", false)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrAborted
	}
	say("✓ Pinned")
	return cert, nil
}

// credentials asks for the username and password and tests the login
// against the pinned certificate, before anything is written.
func (o *Options) credentials(ctx context.Context, say func(string, ...any), defUser string, certPEM []byte) (string, string, error) {
	user, err := o.Prompt.Line("Bridge username (your address)", defUser)
	if err != nil {
		return "", "", err
	}
	user = strings.TrimSpace(user)
	if user == "" {
		return "", "", errors.New("no username given")
	}
	tlsCfg, err := mail.TLSConfigFromPEM(o.Host, certPEM)
	if err != nil {
		return "", "", err
	}
	for try := 1; ; try++ {
		password, err := o.Prompt.Secret("Bridge password (not your Proton password; it is not shown as you type)")
		if err != nil {
			return "", "", err
		}
		c := mail.New(mail.Options{Addr: addr(o.Host, o.IMAPPort), User: user, Password: config.Secret(password), TLSConfig: tlsCfg, Timeout: bridge.Timeout, Logger: o.Logger})
		err = c.Ping(ctx)
		_ = c.Close() // best effort
		if err == nil {
			say("✓ Logged in")
			return user, password, nil
		}
		say("✗ Login failed: %v", err)
		if try == passwordTries {
			return "", "", errors.New("login failed; check the username and the Bridge password in Bridge, and run setup again")
		}
	}
}

func (o *Options) register(say func(string, ...any)) {
	found := false
	for _, c := range o.Clients {
		if !c.Available() {
			continue
		}
		found = true
		if c.Registered() {
			say("✓ %s already has proton-mail-mcp", c.Name())
			continue
		}
		ok, err := o.Prompt.Confirm("Register proton-mail-mcp in "+c.Name()+"?", true)
		if err != nil || !ok {
			say("  Skipped %s", c.Name())
			continue
		}
		if err := c.Register(o.Exe); err != nil {
			say("✗ %s: %v", c.Name(), err)
			continue
		}
		say("✓ Registered in %s (restart it to load the server)", c.Name())
	}
	if !found {
		say("  No Claude Desktop or Claude Code found. To register by hand, see README → Connecting to Claude, with the program at:")
		say("  %s", o.Exe)
	}
}

func addr(host string, port int) string { return net.JoinHostPort(host, strconv.Itoa(port)) }
