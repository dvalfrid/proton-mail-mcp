// Package wizard is the graphical setup: an HTML page in a native web view
// window (WebView2 on Windows) that drives the same steps as the terminal
// setup (package setup), one call at a time.
//
// The page asks; this package decides. The page can only call the methods
// in Backend.Call, and the backend enforces the order: the certificate is
// pinned only when verified or confirmed, nothing is written before the
// login has worked, and registering needs a saved setup. The password goes
// from the page's hidden field to the login test and the OS credential
// store, and nowhere else: it is never logged or sent back to the page.
package wizard

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/dvalfrid/proton-mail-mcp/internal/bridge"
	"github.com/dvalfrid/proton-mail-mcp/internal/config"
	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
	"github.com/dvalfrid/proton-mail-mcp/internal/setup"
)

// Links the page may open in the user's browser, by name. The page can't
// open any other address.
var Links = map[string]string{
	"bridge": "https://proton.me/mail/bridge",
	"help":   "https://github.com/dvalfrid/proton-mail-mcp/blob/main/docs/troubleshooting.md",
}

// Backend holds the state of one wizard run. Its methods are called from the
// page through Call, one at a time.
type Backend struct {
	o       setup.Options
	version string
	accent  Accent
	open    func(url string) error // opens a URL in the user's browser
	quit    func()                 // closes the window

	mu        sync.Mutex
	prev      config.Settings
	prepErr   error
	cert      *setup.Certificate
	confirmed bool
	saved     bool
}

// Accent is the system accent colour, in the shades the page uses in light
// and dark mode ("#rrggbb"; empty for the page's default).
type Accent struct {
	Light string `json:"light"` // for light mode: darker, for contrast on white
	Dark  string `json:"dark"`  // for dark mode: lighter
}

// New returns a backend for o. open opens a URL in the browser and quit
// closes the window; either may be nil in tests.
func New(o setup.Options, version string, accent Accent, open func(string) error, quit func()) *Backend {
	b := &Backend{o: o, version: version, accent: accent, open: open, quit: quit}
	b.prev, b.prepErr = b.o.Prepare()
	return b
}

// SetQuit sets the function that closes the window, once it exists.
func (b *Backend) SetQuit(quit func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.quit = quit
}

// ErrUnknownMethod is a call the page may not make.
var ErrUnknownMethod = errors.New("unknown method")

// Call runs one call from the page. args is the JSON object the page sent.
// The result is sent back to the page as JSON.
func (b *Backend) Call(ctx context.Context, method string, args json.RawMessage) (any, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch method {
	case "state":
		return b.state(), nil
	case "findBridge":
		return b.findBridge(ctx), nil
	case "confirmCertificate":
		return nil, b.confirmCertificate()
	case "signIn":
		var a struct {
			User, Password string
			UseSaved       bool
		}
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, errors.New("bad sign-in request")
		}
		if a.UseSaved {
			return b.signInSaved(ctx, a.User), nil
		}
		return b.signIn(ctx, a.User, a.Password), nil
	case "register":
		var a struct{ Names []string }
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, errors.New("bad register request")
		}
		return b.register(a.Names)
	case "openLink":
		var a struct{ Name string }
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, errors.New("bad link request")
		}
		return nil, b.openLink(a.Name)
	case "close":
		if b.quit != nil {
			b.quit()
		}
		return nil, nil
	}
	return nil, fmt.Errorf("%w %q", ErrUnknownMethod, method)
}

// State is what the page needs before the first step.
type State struct {
	Platform       string        `json:"platform"` // runtime.GOOS
	Version        string        `json:"version"`
	User           string        `json:"user"`          // from an earlier setup
	SavedPassword  bool          `json:"savedPassword"` // the credential store has a password for User; the page never gets it
	Store          string        `json:"store"`
	CanVerifyOwner bool          `json:"canVerifyOwner"`
	Accent         Accent        `json:"accent"`
	Clients        []ClientState `json:"clients"` // the installed ones
	Exe            string        `json:"exe"`     // the server, for registering by hand
	Error          string        `json:"error,omitempty"`
}

// ClientState is one installed MCP client.
type ClientState struct {
	Name       string `json:"name"`
	Registered bool   `json:"registered"`      // it starts this copy
	Other      string `json:"other,omitempty"` // it starts another copy: this one (issue #103)
}

func (b *Backend) state() State {
	s := State{Platform: runtime.GOOS, Version: b.version, User: b.prev.User, Store: storeName(), CanVerifyOwner: setup.CanVerifyOwner(),
		Accent: b.accent, Exe: b.o.Exe, Clients: []ClientState{}}
	if b.prev.User != "" {
		_, err := b.o.Store.Get(b.prev.User)
		s.SavedPassword = err == nil
	}
	if b.prepErr != nil {
		s.Error = "The earlier settings could not be read: " + b.prepErr.Error()
	}
	for _, c := range b.o.Clients {
		if c.Available() {
			cs := ClientState{Name: c.Name()}
			if cmd, ok := c.Registration(); ok {
				if setup.SameExe(cmd, b.o.Exe) {
					cs.Registered = true
				} else {
					cs.Other = cmp.Or(cmd, "another program")
				}
			}
			s.Clients = append(s.Clients, cs)
		}
	}
	return s
}

func storeName() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows Credential Manager"
	case "darwin":
		return "your login keychain"
	default:
		return "your login keyring"
	}
}

// BridgeResult is what findBridge found.
type BridgeResult struct {
	Addr            string `json:"addr"`
	Answers         bool   `json:"answers"`
	IsBridge        bool   `json:"isBridge"`
	Installed       bool   `json:"installed"`
	Fingerprint     string `json:"fingerprint,omitempty"`
	LooksLikeBridge bool   `json:"looksLikeBridge"`
	SMTPProblem     string `json:"smtpProblem,omitempty"`
	OwnerKnown      bool   `json:"ownerKnown"`
	OwnerPath       string `json:"ownerPath,omitempty"`
	Verified        bool   `json:"verified"`
	Error           string `json:"error,omitempty"` // Bridge answers, but the certificate could not be read or SMTP differs
}

// findBridge looks for Bridge and reads its certificate. It starts the
// wizard over: an earlier certificate, confirmation and save are forgotten.
func (b *Backend) findBridge(ctx context.Context) BridgeResult {
	b.cert, b.confirmed, b.saved = nil, false, false
	s := b.o.LookForBridge(ctx)
	r := BridgeResult{Addr: s.Addr, Answers: s.Answers, IsBridge: s.IsBridge, Installed: s.Installed}
	if !s.Answers || !s.IsBridge {
		return r
	}
	c, err := b.o.ReadCertificate(ctx)
	if err != nil {
		r.Error = capitalize(err.Error())
		if errors.Is(err, bridge.ErrNotBridge) {
			r.Error = capitalize(strings.TrimSuffix(err.Error(), ": "+bridge.ErrNotBridge.Error())) + "."
		}
		return r
	}
	b.cert = &c
	r.Fingerprint, r.LooksLikeBridge, r.SMTPProblem, r.Verified = c.Fingerprint, c.LooksLikeBridge, c.SMTPProblem, c.Verified
	if c.OwnerKnown {
		r.OwnerKnown, r.OwnerPath = true, c.Owner.Path
	}
	return r
}

// errOrder means the page skipped a step; it can't happen through the UI.
var errOrder = errors.New("this step needs the one before it")

func (b *Backend) confirmCertificate() error {
	if b.cert == nil {
		return errOrder
	}
	b.confirmed = true
	return nil
}

// SignInResult is the outcome of signIn. The password is never in it.
type SignInResult struct {
	OK           bool   `json:"ok"`
	Error        string `json:"error,omitempty"`
	StoreFailed  bool   `json:"storeFailed,omitempty"` // logged in and saved the settings, but not the password
	SavedFailed  bool   `json:"savedFailed,omitempty"` // there is no saved password, or it no longer works: type it
	SettingsFile string `json:"settingsFile,omitempty"`
}

// signIn tests the login against the pinned certificate and, only when it
// works, saves the settings and the password.
func (b *Backend) signIn(ctx context.Context, user, password string) SignInResult {
	if b.cert == nil || (!b.cert.Verified && !b.confirmed) {
		return SignInResult{Error: "Check Proton Mail Bridge first."}
	}
	user = strings.TrimSpace(user)
	if user == "" || password == "" {
		return SignInResult{Error: "Type the Bridge username and password."}
	}
	if err := b.o.TestLogin(ctx, user, password, *b.cert); err != nil {
		if errors.Is(err, mail.ErrLoginFailed) {
			return SignInResult{Error: "Bridge did not accept the username and password. Check them in Bridge: open your account and choose Mailbox details."}
		}
		return SignInResult{Error: capitalize(err.Error())}
	}
	file, err := b.o.Save(user, password, *b.cert)
	if errors.Is(err, setup.ErrStore) {
		return SignInResult{StoreFailed: true, SettingsFile: file, Error: capitalize(err.Error()) +
			". Unlock or repair it and sign in again, or put " + config.EnvPassword + "=<the Bridge password> in a .env file next to the program, readable only by you."}
	}
	if err != nil {
		return SignInResult{Error: "The settings could not be saved: " + err.Error()}
	}
	b.saved, b.prev.User = true, user
	return SignInResult{OK: true, SettingsFile: file}
}

// signInSaved signs in with the password already in the credential store,
// as signIn does with a typed one. The password stays in this process: the
// page only learns whether it worked.
func (b *Backend) signInSaved(ctx context.Context, user string) SignInResult {
	user = strings.TrimSpace(user)
	if user == "" {
		return SignInResult{Error: "Type the Bridge username."}
	}
	password, err := b.o.Store.Get(user)
	if err != nil || password == "" {
		return SignInResult{SavedFailed: true, Error: "No password is saved for this username. Type the Bridge password."}
	}
	r := b.signIn(ctx, user, password)
	if !r.OK && !r.StoreFailed {
		r.SavedFailed = true
		r.Error = "The saved password didn't work: " + r.Error + " Type the Bridge password below."
	}
	return r
}

// RegisterResult is the outcome for one client.
type RegisterResult struct {
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// register adds the server to the named clients, among the installed ones,
// or switches them to this copy when they start another.
func (b *Backend) register(names []string) ([]RegisterResult, error) {
	if !b.saved {
		return nil, errOrder
	}
	res := []RegisterResult{}
	for _, c := range b.o.Clients {
		if !c.Available() || !contains(names, c.Name()) {
			continue
		}
		r := RegisterResult{Name: c.Name(), OK: true}
		if cmd, ok := c.Registration(); !ok || !setup.SameExe(cmd, b.o.Exe) {
			if err := c.Register(b.o.Exe); err != nil {
				r.OK, r.Error = false, err.Error()
			}
		}
		res = append(res, r)
	}
	return res, nil
}

func (b *Backend) openLink(name string) error {
	url, ok := Links[name]
	if !ok || b.open == nil {
		return fmt.Errorf("no link %q", name)
	}
	return b.open(url)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
