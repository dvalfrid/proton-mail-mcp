package wizard

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/dvalfrid/proton-mail-mcp/internal/bridge"
	"github.com/dvalfrid/proton-mail-mcp/internal/config"
	"github.com/dvalfrid/proton-mail-mcp/internal/keystore"
	"github.com/dvalfrid/proton-mail-mcp/internal/setup"
	"github.com/dvalfrid/proton-mail-mcp/internal/testutil"
)

type fakeClient struct {
	name       string
	registered bool
	got        string
}

func (c *fakeClient) Name() string              { return c.name }
func (*fakeClient) Available() bool             { return true }
func (c *fakeClient) Registered() bool          { return c.registered }
func (c *fakeClient) Register(exe string) error { c.got = exe; return nil }

type fixture struct {
	b      *Backend
	o      setup.Options
	store  keystore.Memory
	client *fakeClient
	opened []string
	calls  []string // every reply, as JSON
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

// newFixture is a wizard against an imitation of Bridge. verified says
// whether the port owner check proves it is Proton's Bridge.
func newFixture(t *testing.T, verified bool) *fixture {
	t.Helper()
	br := testutil.NewBridge(t)
	f := &fixture{store: keystore.Memory{}, client: &fakeClient{name: "Fake Claude"}}
	f.o = setup.Options{
		Store: f.store, Dir: filepath.Join(t.TempDir(), "proton-mail-mcp"), Exe: "/opt/proton-mail-mcp",
		IMAPPort: port(t, br.IMAPAddr), SMTPPort: port(t, br.SMTPAddr), Clients: []setup.Client{f.client},
		PortOwner: func(int) (bridge.Owner, error) {
			if !verified {
				return bridge.Owner{Path: filepath.Join("Temp", "bridge.exe"), Signed: true}, nil
			}
			return bridge.Owner{Path: filepath.Join("Proton AG", "Proton Mail Bridge", "bridge.exe"), Signed: true, ProtectedDir: true}, nil
		},
		Installed: func() (string, bool) { return "", false },
	}
	f.b = New(f.o, "test", Accent{}, func(u string) error { f.opened = append(f.opened, u); return nil }, nil)
	return f
}

// call makes one call as the page would, and decodes the reply into out.
func (f *fixture) call(t *testing.T, method string, args any, out any) error {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.b.Call(context.Background(), method, raw)
	if err != nil {
		return err
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	f.calls = append(f.calls, string(data))
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatal(err)
		}
	}
	return nil
}

type signIn struct{ User, Password string }

func (f *fixture) nothingWritten(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(f.o.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("settings dir was created: %v", err)
	}
	if len(f.store) != 0 {
		t.Error("a password was stored")
	}
}

// The whole wizard: Bridge verified, sign in, register.
func TestWizardHappyPath(t *testing.T) {
	t.Parallel()
	f := newFixture(t, true)

	var st State
	if err := f.call(t, "state", nil, &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Clients) != 1 || st.Clients[0].Name != "Fake Claude" || st.Exe != "/opt/proton-mail-mcp" {
		t.Errorf("state = %+v", st)
	}

	var br BridgeResult
	if err := f.call(t, "findBridge", nil, &br); err != nil {
		t.Fatal(err)
	}
	if !br.Answers || !br.IsBridge || !br.Verified || br.Fingerprint == "" || br.Error != "" {
		t.Fatalf("findBridge = %+v", br)
	}

	var r SignInResult
	if err := f.call(t, "signIn", signIn{testutil.User, testutil.Password}, &r); err != nil {
		t.Fatal(err)
	}
	if !r.OK {
		t.Fatalf("signIn = %+v", r)
	}
	if got, _ := f.store.Get(testutil.User); got != testutil.Password {
		t.Error("the password was not stored")
	}
	s, err := config.ReadSettings(config.SettingsFile(f.o.Dir))
	if err != nil || s.User != testutil.User || s.Fingerprint != br.Fingerprint {
		t.Errorf("settings = %+v, %v", s, err)
	}

	var reg []RegisterResult
	if err := f.call(t, "register", map[string]any{"names": []string{"Fake Claude", "Not installed"}}, &reg); err != nil {
		t.Fatal(err)
	}
	if len(reg) != 1 || !reg[0].OK || f.client.got != "/opt/proton-mail-mcp" {
		t.Errorf("register = %+v, registered %q", reg, f.client.got)
	}

	for _, c := range f.calls {
		if strings.Contains(c, testutil.Password) {
			t.Errorf("a reply to the page holds the password: %s", c)
		}
	}
}

// A certificate that couldn't be verified is used only after the user
// confirms it (issue #71); before that nothing is written.
func TestWizardUnverifiedNeedsConfirmation(t *testing.T) {
	t.Parallel()
	f := newFixture(t, false)
	var br BridgeResult
	if err := f.call(t, "findBridge", nil, &br); err != nil {
		t.Fatal(err)
	}
	if br.Verified || !br.OwnerKnown {
		t.Fatalf("findBridge = %+v, want unverified with a known owner", br)
	}
	var r SignInResult
	if err := f.call(t, "signIn", signIn{testutil.User, testutil.Password}, &r); err != nil {
		t.Fatal(err)
	}
	if r.OK {
		t.Fatal("signed in with an unconfirmed certificate")
	}
	f.nothingWritten(t)

	if err := f.call(t, "confirmCertificate", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.call(t, "signIn", signIn{testutil.User, testutil.Password}, &r); err != nil || !r.OK {
		t.Fatalf("signIn after confirming = %+v, %v", r, err)
	}
}

// Looking for Bridge again forgets an earlier confirmation.
func TestWizardFindBridgeStartsOver(t *testing.T) {
	t.Parallel()
	f := newFixture(t, false)
	_ = f.call(t, "findBridge", nil, nil)
	if err := f.call(t, "confirmCertificate", nil, nil); err != nil {
		t.Fatal(err)
	}
	_ = f.call(t, "findBridge", nil, nil)
	var r SignInResult
	_ = f.call(t, "signIn", signIn{testutil.User, testutil.Password}, &r)
	if r.OK {
		t.Fatal("an earlier confirmation was kept")
	}
	f.nothingWritten(t)
}

// Steps can't be skipped, and a wrong password writes nothing.
func TestWizardEnforcesOrder(t *testing.T) {
	t.Parallel()
	f := newFixture(t, true)
	if err := f.call(t, "confirmCertificate", nil, nil); err == nil {
		t.Error("confirmed a certificate that wasn't read")
	}
	var r SignInResult
	_ = f.call(t, "signIn", signIn{testutil.User, testutil.Password}, &r)
	if r.OK {
		t.Error("signed in before Bridge was found")
	}
	if err := f.call(t, "register", map[string]any{"names": []string{"Fake Claude"}}, nil); err == nil {
		t.Error("registered before setup was saved")
	}

	_ = f.call(t, "findBridge", nil, nil)
	_ = f.call(t, "signIn", signIn{testutil.User, "wrong-pw-5d1a"}, &r)
	if r.OK || r.Error == "" || strings.Contains(r.Error, "wrong-pw-5d1a") {
		t.Errorf("wrong password: %+v", r)
	}
	if err := f.call(t, "register", map[string]any{"names": []string{"Fake Claude"}}, nil); err == nil {
		t.Error("registered after a failed sign-in")
	}
	f.nothingWritten(t)
	if f.client.got != "" {
		t.Error("a client was registered")
	}
}

// Nothing answering is reported, with whether Bridge is installed.
func TestWizardBridgeNotRunning(t *testing.T) {
	t.Parallel()
	f := newFixture(t, true)
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f.o.IMAPPort = port(t, ln.Addr().String())
	_ = ln.Close()
	f.o.Installed = func() (string, bool) { return "somewhere", true }
	f.b = New(f.o, "test", Accent{}, nil, nil)
	var br BridgeResult
	if err := f.call(t, "findBridge", nil, &br); err != nil {
		t.Fatal(err)
	}
	if br.Answers || !br.Installed {
		t.Errorf("findBridge = %+v", br)
	}
}

// The page opens only the named links, and calls only known methods.
func TestWizardRefusesUnknownCalls(t *testing.T) {
	t.Parallel()
	f := newFixture(t, true)
	if err := f.call(t, "openLink", map[string]string{"name": "bridge"}, nil); err != nil || len(f.opened) != 1 || f.opened[0] != Links["bridge"] {
		t.Errorf("openLink bridge: %v, opened %q", err, f.opened)
	}
	if err := f.call(t, "openLink", map[string]string{"name": "https://example.org/"}, nil); err == nil {
		t.Error("opened an address the page made up")
	}
	if err := f.call(t, "exec", nil, nil); !errors.Is(err, ErrUnknownMethod) {
		t.Errorf("unknown method: %v", err)
	}
}

// The page loads nothing from outside itself.
func TestPageIsSelfContained(t *testing.T) {
	t.Parallel()
	if !strings.Contains(Page, `http-equiv="Content-Security-Policy" content="default-src 'none';`) {
		t.Error("the page has no Content-Security-Policy that denies by default")
	}
	for _, u := range regexp.MustCompile(`https?://[^\s"'<>)]+`).FindAllString(Page, -1) {
		if u != "http://www.w3.org/2000/svg" {
			t.Errorf("the page refers to %s; links go through openLink", u)
		}
	}
}

type savedSignIn struct {
	User     string
	UseSaved bool
}

// Run again after a setup, the wizard offers the saved password, and signs
// in with it without the page ever getting it.
func TestWizardSignsInWithSavedPassword(t *testing.T) {
	t.Parallel()
	f := newFixture(t, true)
	_ = f.call(t, "findBridge", nil, nil)
	var r SignInResult
	if err := f.call(t, "signIn", signIn{testutil.User, testutil.Password}, &r); err != nil || !r.OK {
		t.Fatalf("first sign-in = %+v, %v", r, err)
	}

	f.b = New(f.o, "test", Accent{}, nil, nil) // the wizard started again
	var st State
	if err := f.call(t, "state", nil, &st); err != nil {
		t.Fatal(err)
	}
	if st.User != testutil.User || !st.SavedPassword {
		t.Fatalf("state = %+v, want the user and a saved password", st)
	}
	_ = f.call(t, "findBridge", nil, nil)
	if err := f.call(t, "signIn", savedSignIn{testutil.User, true}, &r); err != nil || !r.OK {
		t.Fatalf("sign-in with the saved password = %+v, %v", r, err)
	}
	if err := f.call(t, "register", map[string]any{"names": []string{"Fake Claude"}}, nil); err != nil {
		t.Errorf("register after a saved-password sign-in: %v", err)
	}
	for _, c := range f.calls {
		if strings.Contains(c, testutil.Password) {
			t.Errorf("a reply to the page holds the password: %s", c)
		}
	}
}

// A saved password that no longer works, or none at all, sends the user to
// the password field, and writes nothing.
func TestWizardSavedPasswordFails(t *testing.T) {
	t.Parallel()
	for name, stored := range map[string]string{"wrong": "old-pw-77c2", "none": ""} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, true)
			if stored != "" {
				f.store[testutil.User] = stored
			}
			_ = f.call(t, "findBridge", nil, nil)
			var r SignInResult
			if err := f.call(t, "signIn", savedSignIn{testutil.User, true}, &r); err != nil {
				t.Fatal(err)
			}
			if r.OK || !r.SavedFailed || r.Error == "" || strings.Contains(r.Error, "old-pw-77c2") {
				t.Errorf("signIn = %+v", r)
			}
			if _, err := os.Stat(f.o.Dir); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("settings dir was created: %v", err)
			}
			if got := f.store[testutil.User]; got != stored {
				t.Errorf("stored password changed to %q", got)
			}
		})
	}
}

// The page draws the project icon (assets/icon.svg); the two stay the same.
func TestPageIconIsTheProjectIcon(t *testing.T) {
	t.Parallel()
	svg, err := os.ReadFile(filepath.Join("..", "..", "assets", "icon.svg"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(svg)), "\n") {
		line = strings.TrimSpace(line)
		if line == "</svg>" {
			continue
		}
		if strings.HasPrefix(line, "<svg ") {
			line = strings.TrimSuffix(line, ">") // the page's has aria-hidden
		}
		if !strings.Contains(Page, line) {
			t.Errorf("the page's icon differs from assets/icon.svg at %s", line)
		}
	}
}
