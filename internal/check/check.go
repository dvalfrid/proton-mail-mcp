// Package check diagnoses the connection to Proton Mail Bridge, one step at
// a time, for the `proton-mail-mcp check` command and for start-up. Each
// step says what is wrong and how to fix it. It never reads mail content
// and never prints secrets.
package check

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/dvalfrid/proton-mail-mcp/internal/bridge"
	"github.com/dvalfrid/proton-mail-mcp/internal/config"
	"github.com/dvalfrid/proton-mail-mcp/internal/help"
	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
)

// Status is the outcome of a step.
type Status int

// Step outcomes. Advice is informational: it doesn't fail the check.
const (
	OK Status = iota
	Fail
	Advice
)

// Step is one line of the report.
type Step struct {
	Name   string
	Status Status
	Detail string // what was found
	Hint   string // how to fix it, for Fail and Advice
}

// Input is what the check needs. LoadErr is the error from loading the
// settings, if any; then the other fields are unused.
type Input struct {
	Config       config.Config
	LoadErr      error
	SettingsFile string // where settings were looked for, for the hint
	Logger       *slog.Logger
	// HomeDir, ConfigDir and LocalAppData locate the Claude client configs;
	// empty means the user's own.
	HomeDir, ConfigDir, LocalAppData string
}

// Run performs the steps in order. It stops early when a later step can't
// mean anything (no settings, Bridge not reachable).
func Run(ctx context.Context, in Input) []Step {
	var steps []Step
	add := func(s Step) { steps = append(steps, s) }

	if in.LoadErr != nil {
		add(Step{Name: "settings", Status: Fail, Detail: in.LoadErr.Error(),
			Hint: "run `proton-mail-mcp setup`, or fill in " + in.SettingsFile})
		return steps
	}
	cfg := in.Config
	add(Step{Name: "settings", Status: OK, Detail: "user, password and certificate are set (" + in.SettingsFile + ")"})
	if len(cfg.Options) > 0 {
		lines := make([]string, 0, len(cfg.Options))
		for _, o := range cfg.Options {
			lines = append(lines, o.Name+": "+o.Value+" ("+o.Origin+")")
		}
		add(Step{Name: "options", Status: OK, Detail: strings.Join(lines, "; ")})
	}

	imapAddr := cfg.IMAPAddr()
	greeting, err := bridge.IMAPGreeting(ctx, imapAddr)
	if err != nil {
		add(Step{Name: "Bridge reachable", Status: Fail, Detail: "nothing answers on " + imapAddr,
			Hint: "start Proton Mail Bridge and sign in; if Bridge shows another IMAP port, set " + config.EnvIMAPPort})
		return steps
	}
	add(Step{Name: "Bridge reachable", Status: OK, Detail: imapAddr})

	if bridge.IsBridgeGreeting(greeting) {
		add(Step{Name: "is Proton Mail Bridge", Status: OK, Detail: "the IMAP greeting names Proton Mail Bridge"})
	} else {
		add(Step{Name: "is Proton Mail Bridge", Status: Fail, Detail: "the server on " + imapAddr + " does not identify itself as Proton Mail Bridge",
			Hint: "another program uses this port; check the IMAP port Bridge shows"})
		return steps
	}

	pin, err := readPin(cfg.CertFile)
	if err != nil {
		add(Step{Name: "certificate", Status: Fail, Detail: err.Error(), Hint: "export the certificate from Bridge again"})
		return steps
	}
	served, err := bridge.IMAPCertificate(ctx, imapAddr, cfg.Host)
	switch {
	case err != nil:
		add(Step{Name: "certificate", Status: Fail, Detail: "could not read Bridge's certificate: " + err.Error()})
		return steps
	case bridge.Fingerprint(served) != bridge.Fingerprint(pin):
		add(Step{Name: "certificate", Status: Fail,
			Detail: "Bridge presents " + bridge.Fingerprint(served) + ", but " + bridge.Fingerprint(pin) + " is pinned",
			Hint:   "if Bridge was reset or reinstalled, export its certificate again; otherwise something else may be posing as Bridge"})
		return steps
	default:
		add(Step{Name: "certificate", Status: OK, Detail: "matches the pinned certificate " + bridge.Fingerprint(pin)})
	}

	add(loginStep(ctx, cfg, in.Logger))
	add(smtpStep(ctx, cfg, pin))
	if s, ok := ownerStep(cfg); ok {
		add(s)
	}
	add(registrationStep(in.HomeDir, in.ConfigDir, in.LocalAppData))
	return steps
}

func readPin(path string) (*x509.Certificate, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the operator's own configured certificate
	if err != nil {
		return nil, fmt.Errorf("read pinned certificate: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("no PEM certificate in %s", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse pinned certificate: %w", err)
	}
	return cert, nil
}

func loginStep(ctx context.Context, cfg config.Config, logger *slog.Logger) Step {
	tlsCfg, err := mail.TLSConfig(cfg.Host, cfg.CertFile)
	if err != nil {
		return Step{Name: "login", Status: Fail, Detail: err.Error()}
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	c := mail.New(mail.Options{Addr: cfg.IMAPAddr(), User: cfg.User, Password: cfg.Password, TLSConfig: tlsCfg, Timeout: bridge.Timeout, Logger: logger})
	defer func() { _ = c.Close() }() // best effort
	if err := c.Ping(ctx); err != nil {
		return Step{Name: "login", Status: Fail, Detail: err.Error(),
			Hint: "use the Bridge password from Bridge → your account → Mailbox details (not your Proton password); it changes if the account is removed and added again in Bridge"}
	}
	return Step{Name: "login", Status: OK, Detail: "logged in as " + cfg.User}
}

func smtpStep(ctx context.Context, cfg config.Config, pin *x509.Certificate) Step {
	addr := cfg.SMTPAddr()
	served, err := bridge.SMTPCertificate(ctx, addr, cfg.Host)
	switch {
	case err != nil:
		return Step{Name: "SMTP", Status: Fail, Detail: "SMTP on " + addr + ": " + err.Error(),
			Hint: "if Bridge shows another SMTP port, set " + config.EnvSMTPPort + " (sending isn't used yet, so reading still works)"}
	case bridge.Fingerprint(served) != bridge.Fingerprint(pin):
		return Step{Name: "SMTP", Status: Fail, Detail: "SMTP on " + addr + " presents a different certificate than IMAP",
			Hint: "something other than Bridge may be on the SMTP port"}
	}
	return Step{Name: "SMTP", Status: OK, Detail: addr + ", STARTTLS with the same certificate"}
}

// ownerStep is advice on Windows: which program owns the port, and whether
// it is signed. Elsewhere there is no step.
func ownerStep(cfg config.Config) (Step, bool) {
	if runtime.GOOS != "windows" {
		return Step{}, false
	}
	o, err := bridge.PortOwner(cfg.IMAPPort)
	if err != nil {
		return Step{Name: "port owner", Status: Advice, Detail: "could not tell which program owns the port: " + err.Error()}, true
	}
	if o.TrustedBridge() {
		return Step{Name: "port owner", Status: OK, Detail: o.Path + " (validly signed, in Program Files)"}, true
	}
	return Step{Name: "port owner", Status: Advice, Detail: fmt.Sprintf("%s (signed: %v, in Program Files: %v)", o.Path, o.Signed, o.ProtectedDir),
		Hint: "expected Proton Mail Bridge's bridge.exe, signed and installed under Program Files; check what this program is"}, true
}

// registrationStep looks for the server in the Claude clients' configs. It
// only reports where it is registered; it changes nothing.
func registrationStep(home, configDir, localAppData string) Step {
	var err error
	if home == "" {
		if home, err = os.UserHomeDir(); err != nil {
			return Step{Name: "registered in Claude", Status: Advice, Detail: err.Error()}
		}
	}
	if configDir == "" {
		if configDir, err = os.UserConfigDir(); err != nil {
			return Step{Name: "registered in Claude", Status: Advice, Detail: err.Error()}
		}
	}
	if localAppData == "" {
		localAppData = os.Getenv("LOCALAPPDATA")
	}
	var found, missing []string
	if p := filepath.Join(home, ".claude.json"); registeredIn(p) { // Claude Code, user and local scope
		found = append(found, p)
	}
	// A Claude Desktop config that exists but lacks the server is reported:
	// registered in one Desktop config is not registered in the one the app
	// reads (issue #42).
	for _, d := range config.ClaudeDesktopConfigs(configDir, localAppData) {
		switch {
		case registeredIn(d.Path):
			found = append(found, d.Path)
		case exists(d.Path):
			missing = append(missing, d.Path)
		}
	}
	switch {
	case len(found) == 0:
		return Step{Name: "registered in Claude", Status: Advice, Detail: "no Claude Desktop or Claude Code config names a server for proton-mail-mcp",
			Hint: "run `proton-mail-mcp setup`, or see README → Connecting to Claude"}
	case len(missing) > 0:
		return Step{Name: "registered in Claude", Status: Advice, Detail: "in " + strings.Join(found, ", ") + "; not in " + strings.Join(missing, ", "),
			Hint: "Claude Desktop may read the config that lacks it: run `proton-mail-mcp setup` to register there"}
	}
	return Step{Name: "registered in Claude", Status: OK, Detail: strings.Join(found, ", ")}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// registeredIn reports whether a client config lists a server whose command
// is proton-mail-mcp. Only mcpServers is read; the rest of the file is not
// looked at.
func registeredIn(path string) bool {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the user's own Claude config, read-only
	if err != nil {
		return false
	}
	var cfg struct {
		MCPServers map[string]struct {
			Command string `json:"command"`
		} `json:"mcpServers"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return false
	}
	for _, s := range cfg.MCPServers {
		if strings.Contains(strings.ToLower(filepath.Base(s.Command)), "proton-mail-mcp") {
			return true
		}
	}
	return false
}

// Failed reports whether any step failed.
func Failed(steps []Step) bool {
	for _, s := range steps {
		if s.Status == Fail {
			return true
		}
	}
	return false
}

// Print writes the report, one line per step with its hint below.
func Print(w io.Writer, steps []Step) {
	marks := map[Status]string{OK: "ok  ", Fail: "FAIL", Advice: "note"}
	for _, s := range steps {
		_, _ = fmt.Fprintf(w, "%s  %-22s %s\n", marks[s.Status], s.Name, s.Detail) // a broken terminal can't be reported anywhere
		if s.Hint != "" && s.Status != OK {
			_, _ = fmt.Fprintf(w, "      %-22s → %s\n", "", s.Hint)
		}
	}
	if Failed(steps) {
		_, _ = fmt.Fprintf(w, "\nMore help: %s\n", help.URL(help.Troubleshoot))
	}
}

// ErrBridgeNotReady is logged at start-up when Bridge isn't usable yet. The
// server still starts: Bridge may be started later.
var ErrBridgeNotReady = errors.New("the connection to Proton Mail Bridge is not ready")

// Startup runs the quick, offline-safe part of the check (reachable, is
// Bridge, certificate) and returns the first problem, or nil. It never logs
// in, so it can run on every start.
func Startup(ctx context.Context, cfg config.Config) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addr := cfg.IMAPAddr()
	greeting, err := bridge.IMAPGreeting(ctx, addr)
	if err != nil {
		return fmt.Errorf("%w: nothing answers on %s (start Bridge, or set %s if it uses another port)", ErrBridgeNotReady, addr, config.EnvIMAPPort)
	}
	if !bridge.IsBridgeGreeting(greeting) {
		return fmt.Errorf("%w: the server on %s is not Proton Mail Bridge", ErrBridgeNotReady, addr)
	}
	pin, err := readPin(cfg.CertFile)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBridgeNotReady, err)
	}
	served, err := bridge.IMAPCertificate(ctx, addr, cfg.Host)
	if err != nil {
		return fmt.Errorf("%w: read certificate: %w", ErrBridgeNotReady, err)
	}
	if bridge.Fingerprint(served) != bridge.Fingerprint(pin) {
		return fmt.Errorf("%w: Bridge's certificate is not the pinned one (was Bridge reset or reinstalled?); run `proton-mail-mcp check`", ErrBridgeNotReady)
	}
	return nil
}
