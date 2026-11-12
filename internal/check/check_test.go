package check

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dvalfrid/proton-mail-mcp/internal/config"
	"github.com/dvalfrid/proton-mail-mcp/internal/testutil"
)

func port(t *testing.T, addr string) int {
	t.Helper()
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// setup returns a fake Bridge and settings that match it, with empty Claude
// config dirs.
func setup(t *testing.T) (*testutil.Bridge, Input) {
	t.Helper()
	b := testutil.NewBridge(t)
	cfg := config.Config{
		User: testutil.User, Password: config.Secret(testutil.Password), Host: "127.0.0.1",
		IMAPPort: port(t, b.IMAPAddr), SMTPPort: port(t, b.SMTPAddr), CertFile: b.CertFile(t),
	}
	return b, Input{Config: cfg, SettingsFile: ".env", HomeDir: t.TempDir(), ConfigDir: t.TempDir(), LocalAppData: t.TempDir()}
}

func find(t *testing.T, steps []Step, name string) Step {
	t.Helper()
	for _, s := range steps {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no step %q in %+v", name, steps)
	return Step{}
}

func TestAllGood(t *testing.T) {
	t.Parallel()
	_, in := setup(t)
	steps := Run(context.Background(), in)
	if Failed(steps) {
		var out bytes.Buffer
		Print(&out, steps)
		t.Fatalf("check failed against a good Bridge:\n%s", out.String())
	}
	for _, name := range []string{"settings", "Bridge reachable", "is Proton Mail Bridge", "certificate", "login", "SMTP"} {
		if s := find(t, steps, name); s.Status != OK {
			t.Errorf("%s = %+v", name, s)
		}
	}
	if s := find(t, steps, "registered in Claude"); s.Status != Advice {
		t.Errorf("no Claude config: %+v, want advice", s)
	}
}

func TestFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(t *testing.T, b *testutil.Bridge, in *Input)
		step   string
		want   string // in Detail or Hint
	}{
		{"settings missing", func(_ *testing.T, _ *testutil.Bridge, in *Input) {
			in.LoadErr = errors.New("PROTON_BRIDGE_PASSWORD is not set")
		}, "settings", "PROTON_BRIDGE_PASSWORD"},
		{"nothing listening", func(t *testing.T, _ *testutil.Bridge, in *Input) {
			ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			in.Config.IMAPPort = port(t, ln.Addr().String())
			_ = ln.Close() // now nothing listens there
		}, "Bridge reachable", "start Proton Mail Bridge"},
		{"not Bridge", func(t *testing.T, b *testutil.Bridge, in *Input) {
			in.Config.IMAPPort = port(t, b.Addr()) // the plain IMAP server, without Bridge's greeting
		}, "is Proton Mail Bridge", "another program"},
		{"certificate changed", func(t *testing.T, _ *testutil.Bridge, in *Input) {
			in.Config.CertFile = testutil.NewIMAPServer(t).CertFile(t) // another server's certificate
		}, "certificate", "reset or reinstalled"},
		{"wrong password", func(_ *testing.T, _ *testutil.Bridge, in *Input) {
			in.Config.Password = "not-the-password"
		}, "login", "Bridge password"},
		{"no SMTP", func(t *testing.T, _ *testutil.Bridge, in *Input) {
			ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			in.Config.SMTPPort = port(t, ln.Addr().String())
			_ = ln.Close()
		}, "SMTP", config.EnvSMTPPort},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b, in := setup(t)
			tt.change(t, b, &in)
			steps := Run(context.Background(), in)
			s := find(t, steps, tt.step)
			if s.Status != Fail || !strings.Contains(s.Detail+" "+s.Hint, tt.want) {
				t.Errorf("%s = %+v, want a failure mentioning %q", tt.step, s, tt.want)
			}
			if !Failed(steps) {
				t.Error("Failed() = false")
			}
		})
	}
}

// Guardrail: the report never contains the password, whatever fails.
func TestReportHasNoSecrets(t *testing.T) {
	t.Parallel()
	for _, pw := range []string{testutil.Password, "wrong-password-7c2e"} {
		_, in := setup(t)
		in.Config.Password = config.Secret(pw)
		var out bytes.Buffer
		Print(&out, Run(context.Background(), in))
		if strings.Contains(out.String(), pw) {
			t.Errorf("report contains the password:\n%s", out.String())
		}
	}
}

func TestRegistrationFound(t *testing.T) {
	t.Parallel()
	_, in := setup(t)
	desktop := filepath.Join(in.ConfigDir, "Claude", "claude_desktop_config.json")
	if err := os.MkdirAll(filepath.Dir(desktop), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := `{"mcpServers": {"proton-mail": {"command": "C:\\Tools\\proton-mail-mcp\\proton-mail-mcp.exe"}}}`
	if err := os.WriteFile(desktop, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if s := registrationStep(in.HomeDir, in.ConfigDir, in.LocalAppData); s.Status != OK || !strings.Contains(s.Detail, "claude_desktop_config.json") {
		t.Errorf("registration = %+v", s)
	}
}

// Registered for Claude Code but not in an existing Claude Desktop config is
// a note, not ok: Desktop may read exactly that config (issue #42).
func TestRegistrationMissingInDesktop(t *testing.T) {
	t.Parallel()
	_, in := setup(t)
	code := `{"mcpServers": {"proton-mail": {"command": "/opt/proton-mail-mcp"}}}`
	if err := os.WriteFile(filepath.Join(in.HomeDir, ".claude.json"), []byte(code), 0o600); err != nil {
		t.Fatal(err)
	}
	desktop := filepath.Join(in.ConfigDir, "Claude", "claude_desktop_config.json")
	if err := os.MkdirAll(filepath.Dir(desktop), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(desktop, []byte(`{"preferences": {}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := registrationStep(in.HomeDir, in.ConfigDir, in.LocalAppData)
	if s.Status != Advice || !strings.Contains(s.Detail, "not in "+desktop) {
		t.Errorf("registration = %+v", s)
	}
}

func TestStartup(t *testing.T) {
	t.Parallel()
	b, in := setup(t)
	if err := Startup(context.Background(), in.Config); err != nil {
		t.Errorf("good Bridge: %v", err)
	}
	bad := in.Config
	bad.IMAPPort = port(t, b.Addr())
	if err := Startup(context.Background(), bad); !errors.Is(err, ErrBridgeNotReady) || !strings.Contains(err.Error(), "not Proton Mail Bridge") {
		t.Errorf("not Bridge: %v", err)
	}
	bad = in.Config
	bad.CertFile = testutil.NewIMAPServer(t).CertFile(t)
	if err := Startup(context.Background(), bad); err == nil || !strings.Contains(err.Error(), "pinned") {
		t.Errorf("certificate changed: %v", err)
	}
}
