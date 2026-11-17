package update

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dvalfrid/proton-mail-mcp/internal/fetch"
)

func TestCompare(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		a, b  string
		order int
		ok    bool
	}{
		{"0.5.0", "0.5.0", 0, true},
		{"v0.5.0", "0.5.0", 0, true},
		{"0.5.0", "0.5.1", -1, true},
		{"0.10.0", "0.9.9", 1, true},
		{"1.0.0", "0.99.99", 1, true},
		{"0.6.0-rc.1", "0.6.0", -1, true}, // an RC leads to its release
		{"0.6.0-rc.1", "0.5.0", 1, true},
		{"0.6.0-rc.2", "0.6.0-rc.10", -1, true},
		{"0.6.0-rc.1", "0.6.0-rc.1.1", -1, true},
		{"0.6.0-1", "0.6.0-rc", -1, true},
		{"0.5.0+build", "0.5.0", 0, true},
		{"dev", "0.5.0", 0, false},
		{"0.5", "0.5.0", 0, false},
		{"0.5.x", "0.5.0", 0, false},
	} {
		order, ok := Compare(c.a, c.b)
		if order != c.order || ok != c.ok {
			t.Errorf("Compare(%q, %q) = %d, %v; want %d, %v", c.a, c.b, order, ok, c.order, c.ok)
		}
	}
}

type fixed struct {
	rel fetch.Release
	err error
}

func (f fixed) Latest(context.Context) (fetch.Release, error) { return f.rel, f.err }

func TestCheck(t *testing.T) {
	t.Parallel()
	latest := fixed{rel: fetch.Release{Version: "0.6.0", URL: fetch.ReleasesPage + "/tag/v0.6.0"}}
	for _, c := range []struct {
		running, says string
		update        bool
	}{
		{"0.5.0", "0.6.0 is available; you have 0.5.0", true},
		{"0.6.0-rc.1", "0.6.0 is available; you have 0.6.0-rc.1", true},
		{"0.6.0", "You have the latest version (0.6.0)", false},
		{"0.7.0-rc.1", "a test version newer than the latest release (0.6.0)", false},
		{"dev", "This build has no release version (dev)", false},
	} {
		r, err := Check(context.Background(), latest, c.running)
		if err != nil {
			t.Fatal(err)
		}
		if r.UpdateAvailable != c.update || !strings.Contains(r.Message, c.says) || r.ReleaseURL != latest.rel.URL {
			t.Errorf("Check(%q) = %+v, want update=%v and %q", c.running, r, c.update, c.says)
		}
		if c.update != (r.HowToUpdate != "") || (c.update && !strings.Contains(r.HowToUpdate, "proton-mail-mcp_0.6.0_")) {
			t.Errorf("Check(%q).HowToUpdate = %q", c.running, r.HowToUpdate)
		}
	}

	offline := fixed{err: fetch.ErrNoRelease}
	if _, err := Check(context.Background(), offline, "0.5.0"); !errors.Is(err, fetch.ErrNoRelease) {
		t.Errorf("offline: err = %v", err)
	}
}
