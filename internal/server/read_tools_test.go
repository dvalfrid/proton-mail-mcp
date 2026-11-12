package server

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
	"github.com/dvalfrid/proton-mail-mcp/internal/testutil"
)

const (
	msgFromBob   = "From: Bob Example <bob@example.org>\nTo: alice@example.com\nSubject: Lunch on Friday?\nDate: Mon, 02 Nov 2026 10:00:00 +0000\nMessage-ID: <1@example.org>\n\nAre you free?\n"
	msgMinutes   = "From: Bob Example <bob@example.org>\nTo: team@example.com\nSubject: Minutes\nDate: Thu, 05 Nov 2026 08:00:00 +0000\n\nNotes from the lunch meeting.\n"
	msgFromCarol = "From: carol@example.net\nTo: alice@example.com\nSubject: Invoice 42\nDate: Tue, 03 Nov 2026 09:00:00 +0000\nMessage-ID: <2@example.net>\n\nSee attached.\n"
)

func TestListFolders(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.imap.AddMessage(t, "INBOX", msgFromBob)

	var out ListFoldersOutput
	e.callOK(t, "list_folders", map[string]any{}, &out)

	roles := map[string]string{}
	for _, f := range out.Folders {
		roles[f.Name] = f.Role
		if f.Name == "INBOX" && (f.Messages != 1 || f.Unread != 1) {
			t.Errorf("INBOX = %+v", f)
		}
	}
	// Roles come from Bridge's special-use attributes, not from names.
	want := map[string]string{"INBOX": "inbox", "Trash": "trash", "Drafts": "drafts", "Spam": "spam", "All Mail": "all", "Folders/Work": "", "Labels/Travel": ""}
	for name, role := range want {
		if r, ok := roles[name]; !ok || r != role {
			t.Errorf("%s: role %q (listed %v), want %q", name, r, ok, role)
		}
	}
	if len(out.Folders) != len(testutil.Folders) {
		t.Errorf("folders = %+v", out.Folders)
	}
}

func TestListMessages(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.imap.AddMessage(t, "INBOX", msgFromBob, imap.FlagSeen)
	e.imap.AddMessage(t, "INBOX", msgFromCarol)

	var out ListMessagesOutput
	e.callOK(t, "list_messages", map[string]any{"folder": "INBOX"}, &out)
	if len(out.Messages) != 2 || out.Messages[0].Subject != "Invoice 42" || out.Messages[1].From != "Bob Example <bob@example.org>" {
		t.Fatalf("messages = %+v", out.Messages)
	}
	if out.Note == "" {
		t.Error("result has no untrusted-content note")
	}

	e.callOK(t, "list_messages", map[string]any{"folder": "INBOX", "unread_only": true, "limit": 5}, &out)
	if len(out.Messages) != 1 || out.Messages[0].Subject != "Invoice 42" {
		t.Errorf("unread = %+v", out.Messages)
	}

	e.callOK(t, "list_messages", map[string]any{"folder": "Archive"}, &out)
	if out.Messages == nil || len(out.Messages) != 0 {
		t.Errorf("empty folder = %#v, want an empty list", out.Messages)
	}
}

func TestSearchMessages(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.imap.AddMessage(t, "INBOX", msgFromBob, imap.FlagSeen) // uid 1, 2 Nov
	e.imap.AddMessage(t, "INBOX", msgFromCarol)              // uid 2, 3 Nov
	e.imap.AddMessage(t, "INBOX", msgMinutes)                // uid 3, 5 Nov

	tests := []struct {
		name string
		args map[string]any
		want []uint32
	}{
		{"from", map[string]any{"from": "bob@example.org"}, []uint32{3, 1}},
		{"from, any case", map[string]any{"from": "BOB EXAMPLE"}, []uint32{3, 1}},
		{"to", map[string]any{"to": "team@"}, []uint32{3}},
		{"subject", map[string]any{"subject": "invoice"}, []uint32{2}},
		{"body", map[string]any{"body": "lunch meeting"}, []uint32{3}},
		{"since", map[string]any{"since": "2026-11-03"}, []uint32{3, 2}},
		{"before", map[string]any{"before": "2026-11-03"}, []uint32{1}},
		{"unread", map[string]any{"unread_only": true}, []uint32{3, 2}},
		{"combined", map[string]any{"from": "bob", "unread_only": true}, []uint32{3}},
		{"date range", map[string]any{"since": "2026-11-03", "before": "2026-11-05"}, []uint32{2}},
		{"limit", map[string]any{"since": "2026-01-01", "limit": 2}, []uint32{3, 2}},
		{"no hits", map[string]any{"subject": "nothing like this"}, []uint32{}},
	}
	for _, tt := range tests {
		args := map[string]any{"folder": "INBOX"}
		maps.Copy(args, tt.args)
		var out SearchMessagesOutput
		e.callOK(t, "search_messages", args, &out)
		got := []uint32{}
		for _, m := range out.Messages {
			got = append(got, m.UID)
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s: uids = %v, want %v", tt.name, got, tt.want)
		}
		if out.Messages == nil || out.Note == "" {
			t.Errorf("%s: messages = %#v, note = %q", tt.name, out.Messages, out.Note)
		}
	}
}

// Bridge's SEARCH matches raw, encoded bytes, so non-ASCII terms usually find
// nothing (docs/bridge.md §2). Claude must be told, or it may conclude that
// no such mail exists.
func TestSearchDescribesNonASCIILimitation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	res, err := e.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if tool.Name == "search_messages" {
			if !strings.Contains(tool.Description, "non-ASCII") || !strings.Contains(tool.Description, "not proof") {
				t.Errorf("description doesn't explain the non-ASCII limitation: %q", tool.Description)
			}
			return
		}
	}
	t.Fatal("search_messages not found")
}

func TestSearchMessagesErrors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"no folder", map[string]any{"from": "bob"}, "folder"},
		{"no criteria", map[string]any{"folder": "INBOX"}, "at least one"},
		{"only a limit", map[string]any{"folder": "INBOX", "limit": 5}, "at least one"},
		{"bad date", map[string]any{"folder": "INBOX", "since": "3 Nov 2026"}, "YYYY-MM-DD"},
		{"multi-line term", map[string]any{"folder": "INBOX", "subject": "a\r\nb"}, "single line"},
		{"long term", map[string]any{"folder": "INBOX", "body": strings.Repeat("x", 201)}, "longer than"},
		{"limit too high", map[string]any{"folder": "INBOX", "from": "bob", "limit": 101}, "limit"},
		{"unknown folder", map[string]any{"folder": "Nope", "from": "bob"}, "Nope"},
	}
	for _, tt := range tests {
		res := e.call(t, "search_messages", tt.args)
		if !res.IsError || !strings.Contains(text(res), tt.want) {
			t.Errorf("%s: IsError=%v text=%q, want an error mentioning %q", tt.name, res.IsError, text(res), tt.want)
		}
	}
}

// A search that runs out of time tells Claude how to narrow it (issue #39).
func TestSearchTimeoutAdvises(t *testing.T) {
	t.Parallel()
	silent := testutil.SilentServer(t)
	e := newEnvWith(t, func(o *mail.Options) { o.Addr, o.SearchTimeout = silent, 100*time.Millisecond })
	res := e.call(t, "search_messages", map[string]any{"folder": "INBOX", "from": "bob"})
	if !res.IsError || !strings.Contains(text(res), "since or before") {
		t.Errorf("IsError=%v text=%q, want advice to narrow the search", res.IsError, text(res))
	}
}

func TestReadMessage(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.imap.AddMessage(t, "INBOX", msgFromBob)

	var out ReadMessageOutput
	e.callOK(t, "read_message", map[string]any{"folder": "INBOX", "uid": 1}, &out)
	m := out.UntrustedContent
	if out.Folder != "INBOX" || out.UID != 1 || m == nil {
		t.Fatalf("result = %+v", out)
	}
	if m.Subject != "Lunch on Friday?" || m.Body != "Are you free?" || len(m.From) != 1 || m.From[0] != "Bob Example <bob@example.org>" {
		t.Errorf("message = %+v", m)
	}
	if out.Note != untrustedMessageNote {
		t.Errorf("note = %q", out.Note)
	}
}

// The uidvalidity from a listing, passed back with a uid, makes read_message
// refuse once the folder has been renumbered (Bridge resynced), instead of
// returning whatever message now has that uid.
func TestReadMessageWithUIDValidity(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.imap.AddMessage(t, "Archive", msgFromBob)

	var list ListMessagesOutput
	e.callOK(t, "list_messages", map[string]any{"folder": "Archive"}, &list)
	if list.UIDValidity == 0 || len(list.Messages) != 1 {
		t.Fatalf("listing = %+v", list)
	}
	args := map[string]any{"folder": "Archive", "uid": list.Messages[0].UID, "uidvalidity": list.UIDValidity}
	var out ReadMessageOutput
	e.callOK(t, "read_message", args, &out)

	e.imap.Resync(t, "Archive")
	e.imap.AddMessage(t, "Archive", msgFromCarol)
	res := e.call(t, "read_message", args)
	if !res.IsError || !strings.Contains(text(res), "list it again") || strings.Contains(text(res), "Invoice") {
		t.Errorf("after resync: IsError=%v text=%q, want a renumbered-folder error without content", res.IsError, text(res))
	}

	var found SearchMessagesOutput
	e.callOK(t, "search_messages", map[string]any{"folder": "Archive", "from": "carol"}, &found)
	if found.UIDValidity == 0 || found.UIDValidity == list.UIDValidity {
		t.Errorf("search uidvalidity = %d, listed before resync = %d: want a new non-zero value", found.UIDValidity, list.UIDValidity)
	}
}

func TestReadMessageErrors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.imap.AddMessage(t, "INBOX", msgFromBob)
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"no folder", map[string]any{"uid": 1}, "folder"},
		{"no uid", map[string]any{"folder": "INBOX"}, "uid"},
		{"uid zero", map[string]any{"folder": "INBOX", "uid": 0}, "list_messages"},
		{"unknown uid", map[string]any{"folder": "INBOX", "uid": 99}, "message not found"},
		{"unknown folder", map[string]any{"folder": "Nope", "uid": 1}, "Nope"},
	}
	for _, tt := range tests {
		res := e.call(t, "read_message", tt.args)
		if !res.IsError || !strings.Contains(text(res), tt.want) {
			t.Errorf("%s: IsError=%v text=%q, want an error mentioning %q", tt.name, res.IsError, text(res), tt.want)
		}
	}
}

func TestListMessagesErrors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"no folder", map[string]any{}, "folder"},
		{"empty folder", map[string]any{"folder": ""}, "list_folders"},
		{"limit too high", map[string]any{"folder": "INBOX", "limit": 1000}, "limit"},
		{"unknown folder", map[string]any{"folder": "Nope"}, "Nope"},
	}
	for _, tt := range tests {
		res := e.call(t, "list_messages", tt.args)
		if !res.IsError || !strings.Contains(text(res), tt.want) {
			t.Errorf("%s: IsError=%v text=%q, want an error mentioning %q", tt.name, res.IsError, text(res), tt.want)
		}
	}
}
