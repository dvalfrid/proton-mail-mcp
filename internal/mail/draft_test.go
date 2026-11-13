package mail

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/dvalfrid/proton-mail-mcp/internal/testutil"
)

// The draft is 7-bit MIME: non-ASCII subject and body are encoded, lines
// end in CRLF, and From is the account address.
func TestBuildDraft(t *testing.T) {
	t.Parallel()
	c := New(Options{User: "alice@example.com"})
	raw, err := c.buildDraft(Draft{
		To: []string{"Åsa <asa@example.com>"}, Bcc: []string{"carol@example.net"},
		Subject: "Mötet", Body: "Hej Åsa,\nses på fredag.\n",
	}, time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{
		"From: <alice@example.com>\r\n", "Bcc: <carol@example.net>\r\n", "Subject: =?utf-8?",
		"Content-Type: text/plain; charset=utf-8\r\n", "Content-Transfer-Encoding: quoted-printable\r\n",
		"Message-Id: <", "Hej =C3=85sa,\r\nses p=C3=A5 fredag.\r\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("draft lacks %q:\n%s", want, s)
		}
	}
	for i := range len(s) {
		if s[i] >= 0x80 {
			t.Fatalf("draft has a non-ASCII byte at %d:\n%s", i, s)
		}
	}
}

// oldDraft puts one draft in Drafts and returns its Selection.
func oldDraft(t *testing.T, c *Client, srv *testutil.IMAPServer) Selection {
	t.Helper()
	srv.AddMessage(t, "Drafts", message("alice@example.com", "Old", "Mon, 02 Nov 2026 10:00:00 +0000"), imap.FlagDraft)
	l, err := c.ListMessages(context.Background(), "Drafts", ListOptions{})
	if err != nil || len(l.Messages) != 1 {
		t.Fatalf("list Drafts: %v %+v", err, l)
	}
	return Selection{Folder: "Drafts", UIDs: []uint32{l.Messages[0].UID}, UIDValidity: l.UIDValidity}
}

func fixedBridge(r bridgeRelease) func(*imapclient.Client) (bridgeRelease, error) {
	return func(*imapclient.Client) (bridgeRelease, error) { return r, nil }
}

var newVersion = Draft{To: []string{"bob@example.org"}, Subject: "New", Body: "Hej igen."}

// On Bridge 3.23.1 or later the old draft goes to Trash, never EXPUNGEd.
func TestUpdateDraftTrashesTheOld(t *testing.T) {
	t.Parallel()
	c, srv, _ := newClient(t)
	c.bridgeVersion = fixedBridge(bridgeRelease{3, 23, 1})
	old := oldDraft(t, c, srv)

	r, err := c.UpdateDraft(context.Background(), old.UIDs[0], old.UIDValidity, newVersion)
	if err != nil {
		t.Fatal(err)
	}
	if !r.OldTrashed || r.Kept != "" || r.UID == 0 || r.UID == old.UIDs[0] {
		t.Errorf("UpdateDraft = %+v", r)
	}
	if d, tr := count(t, c, "Drafts"), count(t, c, "Trash"); d != 1 || tr != 1 {
		t.Errorf("Drafts %d, Trash %d; want 1 and 1", d, tr)
	}
	for _, bad := range []string{"EXPUNGE", `\Deleted`} {
		if srv.Sent(bad) {
			t.Errorf("sent a command containing %q", bad)
		}
	}
}

// On an older Bridge, or one that doesn't say, the old draft stays and Kept
// says why: moving it to Trash would delete it permanently.
func TestUpdateDraftKeepsTheOldOnOldBridge(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		version func(*imapclient.Client) (bridgeRelease, error)
		want    string
	}{
		"older":   {fixedBridge(bridgeRelease{3, 23, 0}), "version 3.23.0"},
		"unknown": {askBridgeVersion, "didn't say which version"}, // the test server has no ID
	} {
		c, srv, _ := newClient(t)
		c.bridgeVersion = tc.version
		old := oldDraft(t, c, srv)
		r, err := c.UpdateDraft(context.Background(), old.UIDs[0], old.UIDValidity, newVersion)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if r.OldTrashed || !strings.Contains(r.Kept, tc.want) || !strings.Contains(r.Kept, "still in Drafts") {
			t.Errorf("%s: UpdateDraft = %+v", name, r)
		}
		if d, tr := count(t, c, "Drafts"), count(t, c, "Trash"); d != 2 || tr != 0 {
			t.Errorf("%s: Drafts %d, Trash %d; want 2 and 0", name, d, tr)
		}
		if srv.Sent("MOVE") || srv.Sent("EXPUNGE") {
			t.Errorf("%s: the old draft was moved or expunged", name)
		}
	}
}

// A uid that isn't in Drafts, or a stale uidvalidity, changes nothing: no
// new draft either.
func TestUpdateDraftRefusesAMissingDraft(t *testing.T) {
	t.Parallel()
	c, srv, _ := newClient(t)
	c.bridgeVersion = fixedBridge(bridgeRelease{3, 27, 1})
	old := oldDraft(t, c, srv)
	ctx := context.Background()
	if _, err := c.UpdateDraft(ctx, old.UIDs[0]+10, old.UIDValidity, newVersion); !errors.Is(err, ErrMessagesMissing) {
		t.Errorf("missing uid: %v", err)
	}
	if _, err := c.UpdateDraft(ctx, old.UIDs[0], old.UIDValidity+1, newVersion); !errors.Is(err, ErrUIDValidityChanged) {
		t.Errorf("stale uidvalidity: %v", err)
	}
	if srv.Sent("APPEND") || count(t, c, "Drafts") != 1 {
		t.Error("a refused update created a draft")
	}
}

func TestBridgeVersion(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]bridgeRelease{"03.27.01": {3, 27, 1}, "3.23.1": {3, 23, 1}, "10.00.00": {10, 0, 0}} {
		if got, err := parseBridgeVersion(in); err != nil || got != want {
			t.Errorf("parse %q = %v, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "TODO", "3.23", "3.x.1", "3.-1.0", "1.2.3.4"} {
		if _, err := parseBridgeVersion(in); !errors.Is(err, errVersionUnknown) {
			t.Errorf("parse %q: err = %v", in, err)
		}
	}
	for r, want := range map[bridgeRelease]bool{
		{3, 23, 1}: true, {3, 23, 0}: false, {3, 24, 0}: true, {3, 22, 9}: false, {4, 0, 0}: true, {2, 99, 99}: false,
	} {
		if got := r.atLeast(trashDraftsSince); got != want {
			t.Errorf("%v at least %v = %v", r, trashDraftsSince, got)
		}
	}
}
