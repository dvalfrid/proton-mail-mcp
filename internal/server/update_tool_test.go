package server

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvalfrid/proton-mail-mcp/internal/fetch"
	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
	"github.com/dvalfrid/proton-mail-mcp/internal/update"
)

type fakeReleases struct {
	rel   fetch.Release
	err   error
	calls *int
}

func (f fakeReleases) Latest(context.Context) (fetch.Release, error) {
	*f.calls++
	return f.rel, f.err
}

// session connects a server at version with updates, and a mail client that
// isn't set up (no Bridge needed).
func session(t *testing.T, version string, updates update.Source) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	m := mail.NewUnconfigured(errors.New("no settings"), slog.New(slog.DiscardHandler))
	serverT, clientT := mcp.NewInMemoryTransports()
	ss, err := New(version, m, Folders{}, nil, updates, nil).Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() }) // in-memory: closing cannot fail meaningfully
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() }) // in-memory: closing cannot fail meaningfully
	return cs
}

func TestUpdateTool(t *testing.T) {
	t.Parallel()
	calls := 0
	cs := session(t, "0.5.0", fakeReleases{rel: fetch.Release{Version: "0.6.0", URL: fetch.ReleasesPage + "/tag/v0.6.0"}, calls: &calls})
	if calls != 0 {
		t.Fatalf("GitHub was asked %d times before any call: the server must never check by itself", calls)
	}
	e := &env{session: cs}
	var out UpdateOutput
	e.callOK(t, "proton_mail_mcp_update", map[string]any{}, &out)
	if !out.UpdateAvailable || out.Latest != "0.6.0" || out.Running != "0.5.0" || !strings.Contains(out.HowToUpdate, "0.6.0") || out.Note == "" {
		t.Errorf("update = %+v", out)
	}
	if calls != 1 {
		t.Errorf("GitHub asked %d times, want 1", calls)
	}
}

// Offline: a plain error that says what to do, nothing else.
func TestUpdateToolOffline(t *testing.T) {
	t.Parallel()
	calls := 0
	cs := session(t, "0.5.0", fakeReleases{err: errors.New("couldn't check for updates: the connection failed"), calls: &calls})
	res := (&env{session: cs}).call(t, "proton_mail_mcp_update", map[string]any{})
	if !res.IsError || !strings.Contains(text(res), "couldn't check for updates") || !strings.Contains(text(res), fetch.ReleasesPage) {
		t.Errorf("offline: %s", text(res))
	}
}

// about needs no internet: it never asks GitHub, and says how to update and
// whether Bridge answers.
func TestAboutSaysBridgeAndHowToUpdate(t *testing.T) {
	t.Parallel()
	calls := 0
	cs := session(t, "0.5.0", fakeReleases{calls: &calls})
	var out AboutOutput
	(&env{session: cs}).callOK(t, "about", map[string]any{}, &out)
	if calls != 0 {
		t.Errorf("about asked GitHub %d times", calls)
	}
	if !strings.HasPrefix(out.Bridge, "Not working:") || !strings.Contains(out.Bridge, "Help: ") {
		t.Errorf("bridge, not set up = %q", out.Bridge)
	}
	if out.Releases != fetch.ReleasesPage || !strings.Contains(out.HowToUpdate, fetch.ReleasesPage) {
		t.Errorf("about = %+v", out)
	}

	e := newEnv(t)
	(e).callOK(t, "about", map[string]any{}, &out)
	if !strings.HasPrefix(out.Bridge, "Working") {
		t.Errorf("bridge, working = %q", out.Bridge)
	}
}
