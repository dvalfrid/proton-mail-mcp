package server

import (
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
)

const (
	msgFromBob   = "From: Bob Example <bob@example.org>\nTo: alice@example.com\nSubject: Lunch on Friday?\nDate: Mon, 02 Nov 2026 10:00:00 +0000\nMessage-ID: <1@example.org>\n\nAre you free?\n"
	msgFromCarol = "From: carol@example.net\nTo: alice@example.com\nSubject: Invoice 42\nDate: Tue, 03 Nov 2026 09:00:00 +0000\nMessage-ID: <2@example.net>\n\nSee attached.\n"
)

func TestListFolders(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.imap.AddMessage(t, "INBOX", msgFromBob)

	var out ListFoldersOutput
	e.callOK(t, "list_folders", map[string]any{}, &out)

	found := false
	for _, f := range out.Folders {
		if f.Name == "INBOX" {
			found = true
			if f.Role != "inbox" || f.Messages != 1 || f.Unread != 1 {
				t.Errorf("INBOX = %+v", f)
			}
		}
	}
	if !found || len(out.Folders) != 7 {
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
