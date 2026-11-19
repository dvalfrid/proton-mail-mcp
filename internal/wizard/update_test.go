package wizard

import (
	"context"
	"errors"
	"testing"

	"github.com/dvalfrid/proton-mail-mcp/internal/config"
	"github.com/dvalfrid/proton-mail-mcp/internal/fetch"
	"github.com/dvalfrid/proton-mail-mcp/internal/update"
)

type fakeReleases struct {
	rel   fetch.Release
	list  []fetch.Release // for the test channel
	err   error
	calls int
}

func (f *fakeReleases) Releases(context.Context) ([]fetch.Release, error) {
	f.calls++
	return f.list, f.err
}

func (f *fakeReleases) Latest(context.Context) (fetch.Release, error) {
	f.calls++
	return f.rel, f.err
}

// GitHub is asked only on the owner's click; the release page then opens
// through openLink, the only way the page opens anything.
func TestCheckForUpdates(t *testing.T) {
	t.Parallel()
	f := newFixture(t, true)
	rel := &fakeReleases{rel: fetch.Release{Version: "0.6.0", URL: fetch.ReleasesPage + "/tag/v0.6.0"}}
	f.b.updates = rel
	f.b.version = "0.5.0"

	if err := f.call(t, "openLink", map[string]string{"name": "release"}, nil); err != nil || f.opened[0] != fetch.ReleasesPage {
		t.Fatalf("release before a check: %v, opened %q", err, f.opened)
	}
	for _, m := range []string{"state", "runCheck", "getSettings"} {
		_ = f.call(t, m, map[string]any{}, nil) // results are other tests' concern
	}
	if rel.calls != 0 {
		t.Fatalf("GitHub was asked %d times without a click", rel.calls)
	}

	var r update.Result
	if err := f.call(t, "checkUpdate", map[string]any{}, &r); err != nil {
		t.Fatal(err)
	}
	if rel.calls != 1 || !r.UpdateAvailable || r.Latest != "0.6.0" || r.Running != "0.5.0" || r.HowToUpdate == "" {
		t.Errorf("checkUpdate = %+v after %d calls", r, rel.calls)
	}
	if err := f.call(t, "openLink", map[string]string{"name": "release"}, nil); err != nil || f.opened[1] != rel.rel.URL {
		t.Errorf("release after a check: %v, opened %q", err, f.opened)
	}

	rel.err = errors.New("couldn't check for updates: the connection failed")
	if err := f.call(t, "checkUpdate", map[string]any{}, nil); err == nil {
		t.Error("offline: no error")
	}
}

// With the test channel on (the Settings page), Check for updates offers a
// release candidate (issue #213).
func TestCheckForUpdatesTestChannel(t *testing.T) {
	t.Parallel()
	f := newFixture(t, true)
	rel := &fakeReleases{
		rel:  fetch.Release{Version: "0.6.0", URL: fetch.ReleasesPage + "/tag/v0.6.0"},
		list: []fetch.Release{{Version: "0.6.0"}, {Version: "0.7.0-rc.1", URL: fetch.ReleasesPage + "/tag/v0.7.0-rc.1", Candidate: true}},
	}
	f.b.updates = rel
	f.b.version = "0.6.0"
	if err := config.WriteSettings(config.SettingsFile(f.o.Dir), config.Settings{User: "alice@example.com"}); err != nil {
		t.Fatal(err)
	}
	var r update.Result
	if err := f.call(t, "checkUpdate", map[string]any{}, &r); err != nil || r.UpdateAvailable {
		t.Fatalf("stable: %+v, %v", r, err)
	}
	var s Settings
	if err := f.call(t, "saveSettings", map[string]any{"maxChanges": 5, "testChannel": true}, &s); err != nil || !s.TestChannel {
		t.Fatalf("switch on: %+v, %v", s, err)
	}
	if err := f.call(t, "checkUpdate", map[string]any{}, &r); err != nil || !r.UpdateAvailable || r.Latest != "0.7.0-rc.1" || !r.Candidate {
		t.Errorf("test channel: %+v, %v", r, err)
	}
	if err := f.call(t, "openLink", map[string]string{"name": "release"}, nil); err != nil || f.opened[len(f.opened)-1] != fetch.ReleasesPage+"/tag/v0.7.0-rc.1" {
		t.Errorf("release page: %v, opened %q", err, f.opened)
	}
}
