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
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"

	"github.com/dvalfrid/proton-mail-mcp/internal/bridge"
	"github.com/dvalfrid/proton-mail-mcp/internal/config"
	"github.com/dvalfrid/proton-mail-mcp/internal/keystore"
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
	say := func(format string, args ...any) { _, _ = fmt.Fprintf(o.Out, format+"\n", args...) } // a broken terminal can't be reported anywhere

	prev, err := o.Prepare()
	if err != nil {
		return err
	}

	say("Proton Mail Bridge")
	if err := o.findBridge(ctx, say); err != nil {
		return err
	}

	cert, err := o.pinCertificate(ctx, say)
	if err != nil {
		return err
	}

	say("")
	say("Bridge account (Bridge → your account → Mailbox details)")
	user, password, err := o.credentials(ctx, say, cmpOr(prev.User, ""), cert)
	if err != nil {
		return err
	}

	file, err := o.Save(user, password, cert)
	if errors.Is(err, ErrStore) {
		say("✗ %s", capitalize(err.Error()))
		// Not in the MCP client's config: STANDARDS keeps the password out
		// of it (issue #74).
		say("  Put %s=<the Bridge password> in a .env file next to the program, readable only by you, or fix the credential store and run setup again.", config.EnvPassword)
		return err
	}
	if err != nil {
		return err
	}
	say("✓ Saved: settings in %s, the password in the OS credential store", file)

	say("")
	say("Claude")
	o.register(say)
	say("")
	say("Done. Run `proton-mail-mcp check` at any time to test the connection.")
	return nil
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
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
		s := o.LookForBridge(ctx)
		switch {
		case s.Answers && !s.IsBridge:
			say("✗ Something answers on %s, but it is not Proton Mail Bridge.", s.Addr)
			say("  Check the IMAP port that Bridge shows, and run setup again with -imap-port.")
			return bridge.ErrNotBridge
		case s.Answers:
			say("✓ Bridge answers on %s", s.Addr)
			return nil
		case s.Installed:
			say("✗ Nothing answers on %s. Bridge is installed (%s) but doesn't seem to be running.", s.Addr, s.InstalledAt)
			say("  Start Proton Mail Bridge and sign in.")
		default:
			say("✗ Nothing answers on %s, and Proton Mail Bridge doesn't seem to be installed.", s.Addr)
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
func (o *Options) pinCertificate(ctx context.Context, say func(string, ...any)) (Certificate, error) {
	c, err := o.ReadCertificate(ctx)
	if errors.Is(err, bridge.ErrNotBridge) {
		say("✗ %s", capitalize(strings.TrimSuffix(err.Error(), ": "+bridge.ErrNotBridge.Error())))
		return Certificate{}, err
	}
	if err != nil {
		return Certificate{}, err
	}
	if !c.LooksLikeBridge {
		say("! The certificate is not the kind Bridge makes (issued to %s).", c.cert.Subject)
	}
	if c.SMTPProblem != "" {
		say("! %s. Reading mail works without it.", c.SMTPProblem)
	}
	say("  Certificate SHA-256: %s", c.Fingerprint)
	if c.OwnerKnown {
		say("  The port belongs to %s (validly signed: %v, in Program Files: %v)", c.Owner.Path, c.Owner.Signed, c.Owner.ProtectedDir)
	}
	if c.Verified {
		say("✓ Pinned: the port is held by a signed bridge.exe installed in Program Files")
		return c, nil
	}
	if CanVerifyOwner() {
		say("! Could not confirm that the program on the port is Proton Mail Bridge as installed in Program Files.")
		say("  Answer yes only if that path is where you installed Proton Mail Bridge.")
	}
	ok, err := o.Prompt.Confirm("Is Proton Mail Bridge running and signed in on this computer, so this is its certificate?", false)
	if err != nil {
		return Certificate{}, err
	}
	if !ok {
		return Certificate{}, ErrAborted
	}
	say("✓ Pinned")
	return c, nil
}

// credentials asks for the username and password and tests the login
// against the pinned certificate, before anything is written.
func (o *Options) credentials(ctx context.Context, say func(string, ...any), defUser string, c Certificate) (string, string, error) {
	user, err := o.Prompt.Line("Bridge username (your address)", defUser)
	if err != nil {
		return "", "", err
	}
	user = strings.TrimSpace(user)
	if user == "" {
		return "", "", errors.New("no username given")
	}
	for try := 1; ; try++ {
		password, err := o.Prompt.Secret("Bridge password (not your Proton password; it is not shown as you type)")
		if err != nil {
			return "", "", err
		}
		err = o.TestLogin(ctx, user, password, c)
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
