package mail

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	gomail "github.com/emersion/go-message/mail"

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

// With attachments the draft is multipart/mixed: the text first, then each
// file in base64 under its name, which reads back as the same bytes.
func TestBuildDraftWithAttachments(t *testing.T) {
	t.Parallel()
	c := New(Options{User: "alice@example.com"})
	pdf := []byte("%PDF-1.7\n\x00\xff binary")
	raw, err := c.buildDraft(Draft{
		To: []string{"bob@example.org"}, Subject: "Guide", Body: "Här kommer filen.\n",
		Attachments: []DraftFile{
			{Name: "Guide för Åsa.pdf", ContentType: "application/pdf", Data: pdf},
			{Name: "notes.txt", ContentType: "text/plain", Data: []byte("hej")},
		},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for i := range raw {
		if raw[i] >= 0x80 {
			t.Fatalf("draft has a non-ASCII byte at %d", i)
		}
	}

	r, err := gomail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "multipart/mixed") {
		t.Errorf("Content-Type %q", ct)
	}
	var text string
	var files []DraftFile
	for {
		p, err := r.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(p.Body)
		if err != nil {
			t.Fatal(err)
		}
		switch h := p.Header.(type) {
		case *gomail.InlineHeader:
			text = string(data)
		case *gomail.AttachmentHeader:
			name, _ := h.Filename()
			ct, _, _ := h.ContentType()
			files = append(files, DraftFile{Name: name, ContentType: ct, Data: data})
		}
	}
	if text != "Här kommer filen.\r\n" {
		t.Errorf("text %q", text)
	}
	if len(files) != 2 || files[0].Name != "Guide för Åsa.pdf" || files[0].ContentType != "application/pdf" ||
		!bytes.Equal(files[0].Data, pdf) || files[1].Name != "notes.txt" || string(files[1].Data) != "hej" {
		t.Errorf("attachments %+v", files)
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

// trash_message moves a draft to Trash only on Bridge 3.23.1 or later, and
// otherwise refuses before anything changes, saying why.
func TestTrashDrafts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, tc := range map[string]struct {
		version func(*imapclient.Client) (bridgeRelease, error)
		moved   bool
		want    string
	}{
		"new":     {fixedBridge(bridgeRelease{3, 23, 1}), true, ""},
		"older":   {fixedBridge(bridgeRelease{3, 22, 0}), false, "version 3.22.0"},
		"unknown": {askBridgeVersion, false, "didn't say which version"},
	} {
		c, srv, _ := newClient(t)
		c.bridgeVersion = tc.version
		old := oldDraft(t, c, srv)
		n, err := c.Trash(ctx, old)
		if tc.moved {
			if err != nil || n != 1 || count(t, c, "Trash") != 1 || count(t, c, "Drafts") != 0 {
				t.Errorf("%s: Trash = %d, %v", name, n, err)
			}
		} else {
			if !errors.Is(err, ErrNotAllowed) || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "update Bridge") {
				t.Errorf("%s: err = %v", name, err)
			}
			if srv.Sent("MOVE") || count(t, c, "Drafts") != 1 {
				t.Errorf("%s: the draft was moved", name)
			}
		}
		if srv.Sent("EXPUNGE") || srv.Sent(`\Deleted`) {
			t.Errorf("%s: sent EXPUNGE or a deleted flag", name)
		}
	}
}
