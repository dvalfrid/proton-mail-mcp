package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dvalfrid/proton-mail-mcp/internal/bridge"
	"github.com/dvalfrid/proton-mail-mcp/internal/config"
	"github.com/dvalfrid/proton-mail-mcp/internal/keystore"
	"github.com/dvalfrid/proton-mail-mcp/internal/testutil"
)

// script answers prompts in order and records them.
type script struct {
	lines    []string
	secrets  []string
	confirms []bool
	asked    []string
}

func (s *script) Line(prompt, def string) (string, error) {
	s.asked = append(s.asked, "line: "+prompt)
	if len(s.lines) == 0 {
		return "", errors.New("unexpected question: " + prompt)
	}
	a := s.lines[0]
	s.lines = s.lines[1:]
	if a == "" {
		return def, nil
	}
	return a, nil
}

func (s *script) Secret(prompt string) (string, error) {
	s.asked = append(s.asked, "secret: "+prompt)
	if len(s.secrets) == 0 {
		return "", errors.New("unexpected password question")
	}
	a := s.secrets[0]
	s.secrets = s.secrets[1:]
	return a, nil
}

func (s *script) Confirm(prompt string, _ bool) (bool, error) {
	s.asked = append(s.asked, "confirm: "+prompt)
	if len(s.confirms) == 0 {
		return false, errors.New("unexpected question: " + prompt)
	}
	a := s.confirms[0]
	s.confirms = s.confirms[1:]
	return a, nil
}

// fakeClient records registrations.
type fakeClient struct {
	available, registered bool
	got                   string
}

func (*fakeClient) Name() string                { return "Fake Claude" }
func (c *fakeClient) Available() bool           { return c.available }
func (c *fakeClient) Registered() bool          { return c.registered }
func (c *fakeClient) Register(exe string) error { c.got = exe; return nil }
func (c *fakeClient) Unregister(exe string) (bool, error) {
	if c.got != exe {
		return false, nil
	}
	c.got = ""
	return true, nil
}

func port(t *testing.T, addr string) int {
	t.Helper()
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(p)
	return n
}

type fixture struct {
	b      *testutil.Bridge
	o      Options
	out    *bytes.Buffer
	store  keystore.Memory
	client *fakeClient
	ask    *script
}

func newFixture(t *testing.T, ask *script) *fixture {
	t.Helper()
	b := testutil.NewBridge(t)
	f := &fixture{b: b, out: &bytes.Buffer{}, store: keystore.Memory{}, client: &fakeClient{available: true}, ask: ask}
	f.o = Options{
		Out: f.out, Prompt: ask, Store: f.store, Dir: filepath.Join(t.TempDir(), "proton-mail-mcp"),
		Exe: "/opt/proton-mail-mcp", IMAPPort: port(t, b.IMAPAddr), SMTPPort: port(t, b.SMTPAddr),
		Clients:   []Client{f.client},
		PortOwner: func(int) (bridge.Owner, error) { return bridge.Owner{}, errors.New("not checked in tests") },
		Installed: func() (string, bool) { return "", false },
	}
	return f
}

func TestSetupHappyPath(t *testing.T) {
	t.Parallel()
	f := newFixture(t, &script{confirms: []bool{true, true}, lines: []string{testutil.User}, secrets: []string{testutil.Password}})
	if err := Run(context.Background(), f.o); err != nil {
		t.Fatalf("%v\n%s", err, f.out)
	}

	s, err := config.ReadSettings(config.SettingsFile(f.o.Dir))
	if err != nil {
		t.Fatal(err)
	}
	if s.User != testutil.User || s.IMAPPort != f.o.IMAPPort || s.SMTPPort != f.o.SMTPPort || s.Host != "127.0.0.1" || s.Fingerprint == "" {
		t.Errorf("settings = %+v", s)
	}
	pinned, err := os.ReadFile(s.CertFile)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := os.ReadFile(f.b.CertFile(t))
	if !bytes.Equal(pinned, want) {
		t.Error("the pinned certificate is not the one Bridge presents")
	}
	if pw, _ := f.store.Get(testutil.User); pw != testutil.Password {
		t.Error("password not in the credential store")
	}
	if f.client.got != "/opt/proton-mail-mcp" {
		t.Errorf("registered %q", f.client.got)
	}
	// The saved settings and the stored password are enough for the server.
	files := config.FromMap(s.Values())
	cfg, err := config.Load(config.Chain(files, config.PasswordFrom(files, f.store.Get)))
	if err != nil || cfg.CertFile != s.CertFile {
		t.Errorf("loading the saved settings: %+v, %v", cfg, err)
	}
}

// Guardrail: the password is never printed, and never written to the
// settings file.
func TestSetupNeverShowsOrFilesThePassword(t *testing.T) {
	t.Parallel()
	f := newFixture(t, &script{confirms: []bool{true, false}, lines: []string{testutil.User}, secrets: []string{"wrong-pw-5d1a", testutil.Password}})
	if err := Run(context.Background(), f.o); err != nil {
		t.Fatalf("%v\n%s", err, f.out)
	}
	for _, pw := range []string{testutil.Password, "wrong-pw-5d1a"} {
		if strings.Contains(f.out.String(), pw) {
			t.Errorf("output contains a password:\n%s", f.out)
		}
	}
	entries, err := os.ReadDir(f.o.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(f.o.Dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(testutil.Password)) {
			t.Errorf("%s contains the password", e.Name())
		}
	}
	if !strings.Contains(f.out.String(), "Login failed") {
		t.Errorf("the wrong password wasn't reported:\n%s", f.out)
	}
	if f.client.got != "" {
		t.Error("registered although the user said no")
	}
}

// When the Windows check proves the port is Proton's signed bridge.exe, the
// certificate is pinned without asking.
func TestSetupPinsWithoutAskingWhenOwnerIsVerified(t *testing.T) {
	t.Parallel()
	ask := &script{confirms: []bool{true}, lines: []string{testutil.User}, secrets: []string{testutil.Password}}
	f := newFixture(t, ask)
	f.o.PortOwner = func(int) (bridge.Owner, error) {
		// filepath.Join, so the path splits on this OS's separator: the real
		// owner check only exists on Windows, but the test runs everywhere.
		return bridge.Owner{Path: filepath.Join("Proton AG", "Proton Mail Bridge", "bridge.exe"), Signed: true, ProtectedDir: true}, nil
	}
	if err := Run(context.Background(), f.o); err != nil {
		t.Fatalf("%v\n%s", err, f.out)
	}
	for _, q := range ask.asked {
		if strings.Contains(q, "certificate") {
			t.Errorf("asked about the certificate: %q", q)
		}
	}
}

// A signed program renamed bridge.exe outside Program Files is not trusted:
// setup asks before pinning its certificate (issue #71).
func TestSetupAsksForASignedImpostor(t *testing.T) {
	t.Parallel()
	ask := &script{confirms: []bool{false}}
	f := newFixture(t, ask)
	f.o.PortOwner = func(int) (bridge.Owner, error) {
		return bridge.Owner{Path: filepath.Join("Temp", "bridge.exe"), Signed: true}, nil
	}
	if err := Run(context.Background(), f.o); !errors.Is(err, ErrAborted) {
		t.Fatalf("err = %v, want ErrAborted after the user said no\n%s", err, f.out)
	}
	if !slices.ContainsFunc(ask.asked, func(q string) bool { return strings.Contains(q, "certificate") }) {
		t.Errorf("setup didn't ask about the certificate: %q", ask.asked)
	}
	if _, err := os.Stat(f.o.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Error("settings were written")
	}
}

// Nothing is written unless the setup completes.
func TestSetupStopsWithoutWriting(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		ask    *script
		change func(t *testing.T, f *fixture)
		want   error
	}{
		{"user declines the certificate", &script{confirms: []bool{false}}, nil, ErrAborted},
		{"not Bridge", &script{}, func(t *testing.T, f *fixture) { f.o.IMAPPort = port(t, f.b.Addr()) }, bridge.ErrNotBridge},
		{"nothing listening, user quits", &script{lines: []string{"q"}}, func(t *testing.T, f *fixture) {
			ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			f.o.IMAPPort = port(t, ln.Addr().String())
			_ = ln.Close()
		}, ErrAborted},
		{"wrong password every time", &script{confirms: []bool{true}, lines: []string{testutil.User}, secrets: []string{"a", "b", "c"}}, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, tt.ask)
			if tt.change != nil {
				tt.change(t, f)
			}
			err := Run(context.Background(), f.o)
			if err == nil || (tt.want != nil && !errors.Is(err, tt.want)) {
				t.Fatalf("err = %v, want %v\n%s", err, tt.want, f.out)
			}
			if _, err := os.Stat(f.o.Dir); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("settings dir was created: %v", err)
			}
			if len(f.store) != 0 {
				t.Error("a password was stored")
			}
		})
	}
}

// A port typed after "nothing answers" is used.
func TestSetupAsksForAnotherPort(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	real := f.o.IMAPPort
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f.o.IMAPPort = port(t, ln.Addr().String())
	_ = ln.Close()
	f.o.Prompt = &script{lines: []string{strconv.Itoa(real), testutil.User}, confirms: []bool{true, false}, secrets: []string{testutil.Password}}
	if err := Run(context.Background(), f.o); err != nil {
		t.Fatalf("%v\n%s", err, f.out)
	}
	if s, _ := config.ReadSettings(config.SettingsFile(f.o.Dir)); s.IMAPPort != real {
		t.Errorf("saved IMAP port %d, want %d", s.IMAPPort, real)
	}
	if !strings.Contains(f.out.String(), "doesn't seem to be installed") {
		t.Errorf("no install hint:\n%s", f.out)
	}
}

func TestClaudeDesktopMerge(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "Claude", "claude_desktop_config.json")
	d := ClaudeDesktop{Path: path, Label: "Claude Desktop"}
	if d.Available() {
		t.Error("available without its directory")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	orig := `{"mcpServers": {"other": {"command": "other.exe", "args": ["-x"]}}, "theme": "dark"}`
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	if !d.Available() || d.Registered() {
		t.Fatal("state before registering is wrong")
	}
	if err := d.Register("/opt/proton-mail-mcp"); err != nil {
		t.Fatal(err)
	}
	if !d.Registered() {
		t.Error("not registered after Register")
	}
	var got map[string]any
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	servers := got["mcpServers"].(map[string]any)
	if got["theme"] != "dark" || servers["other"] == nil || servers[ServerName].(map[string]any)["command"] != "/opt/proton-mail-mcp" {
		t.Errorf("merged config = %s", data)
	}
	if bak, err := os.ReadFile(path + ".bak"); err != nil || string(bak) != orig {
		t.Errorf("backup = %q, %v", bak, err)
	}

	// A config that isn't valid JSON is left alone.
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := d.Register("/opt/proton-mail-mcp"); err == nil {
		t.Error("registered into an invalid config")
	}
	if data, _ := os.ReadFile(path); string(data) != "{not json" {
		t.Error("an invalid config was changed")
	}
}

// failingStore can't save, like a locked keychain.
type failingStore struct{}

func (failingStore) Get(string) (string, error) { return "", keystore.ErrNotFound }
func (failingStore) Set(string, string) error   { return errors.New("the keychain is locked") }

// When the credential store can't save, setup's advice never points at the
// MCP client's config, which must not hold the password (issue #74).
func TestSetupAdviceWhenTheStoreFails(t *testing.T) {
	t.Parallel()
	f := newFixture(t, &script{confirms: []bool{true}, lines: []string{testutil.User}, secrets: []string{testutil.Password}})
	f.o.Store = failingStore{}
	if err := Run(context.Background(), f.o); err == nil {
		t.Fatal("setup succeeded without storing the password")
	}
	out := f.out.String()
	if !strings.Contains(out, ".env file next to the program") || strings.Contains(out, "MCP client starts") {
		t.Errorf("advice:\n%s", out)
	}
}
