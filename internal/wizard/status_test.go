package wizard

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/dvalfrid/proton-mail-mcp/internal/check"
	"github.com/dvalfrid/proton-mail-mcp/internal/help"
	"github.com/dvalfrid/proton-mail-mcp/internal/testutil"
)

// noProtonEnv skips a test that loads settings as the server does when the
// developer's own PROTON_* variables would override the test's.
func noProtonEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "PROTON_") {
			t.Skip("a PROTON_* variable is set, which would override the test's settings")
		}
	}
}

// Before setup the window opens the setup steps, and the Status page says
// it isn't set up, with its help section.
func TestStatusBeforeSetup(t *testing.T) {
	t.Parallel()
	noProtonEnv(t)
	f := newFixture(t, true)
	var st State
	if err := f.call(t, "state", nil, &st); err != nil || st.Mode != "setup" || len(st.Tools) == 0 {
		t.Fatalf("state = %+v, %v", st, err)
	}
	var r StatusResult
	if err := f.call(t, "runCheck", nil, &r); err != nil {
		t.Fatal(err)
	}
	if !r.Failed || len(r.Steps) != 1 || r.Steps[0].Name != "settings" || r.Steps[0].Status != "fail" ||
		r.Steps[0].Help != string(help.NotSetUp) || r.Steps[0].Hint == "" || r.Steps[0].Title == "" {
		t.Errorf("runCheck = %+v", r)
	}
	f.nothingWritten(t)
}

// Once set up, the window opens the app pages, and the Status page shows
// exactly what `proton-mail-mcp check` reports for the same settings (issue
// #176): both run check.Diagnose. The password is never in it.
func TestStatusIsTheCheck(t *testing.T) {
	t.Parallel()
	noProtonEnv(t)
	f := newFixture(t, true)
	if err := f.call(t, "findBridge", nil, nil); err != nil {
		t.Fatal(err)
	}
	var in SignInResult
	if err := f.call(t, "signIn", signIn{testutil.User, testutil.Password}, &in); err != nil || !in.OK {
		t.Fatalf("signIn = %+v, %v", in, err)
	}
	var st State
	if err := f.call(t, "state", nil, &st); err != nil || st.Mode != "app" || st.Start != "" {
		t.Fatalf("state after setup = %+v, %v", st, err)
	}

	var r StatusResult
	if err := f.call(t, "runCheck", nil, &r); err != nil {
		t.Fatal(err)
	}
	if r.Failed {
		t.Fatalf("a good setup fails: %+v", r)
	}
	for _, s := range r.Steps {
		if s.Title == "" || (s.Status == "fail" && s.Hint == "") {
			t.Errorf("step %+v", s)
		}
	}
	want := statusResult(check.Diagnose(context.Background(), check.EnvFile(f.o.Exe), f.o.Dir, f.store.Get))
	// The port owner and Claude configs are the machine's own: only compare
	// what both runs see the same way.
	if !reflect.DeepEqual(names(r), names(want)) || r.Failed != want.Failed {
		t.Errorf("Status page %v, check %v", names(r), names(want))
	}
	for _, c := range f.calls {
		if strings.Contains(c, testutil.Password) {
			t.Errorf("a reply holds the password: %s", c)
		}
	}
}

func names(r StatusResult) []string {
	var out []string
	for _, s := range r.Steps {
		out = append(out, s.Name+"="+s.Status)
	}
	return out
}

// A failing step names its help section, which the page may open; no
// other topic is opened.
func TestStatusHelpLinks(t *testing.T) {
	t.Parallel()
	f := newFixture(t, true)
	if err := f.call(t, "openLink", map[string]string{"name": "help:" + string(help.BridgeDown)}, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.opened) != 1 || f.opened[0] != help.URL(help.BridgeDown) {
		t.Errorf("opened %v", f.opened)
	}
	for _, bad := range []string{"help:no-such-topic", "help:../../evil", "https://example.org"} {
		if err := f.call(t, "openLink", map[string]string{"name": bad}, nil); err == nil {
			t.Errorf("%q was opened", bad)
		}
	}
	if len(f.opened) != 1 {
		t.Errorf("opened %v", f.opened)
	}
}
