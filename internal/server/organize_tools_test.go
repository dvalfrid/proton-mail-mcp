package server

import (
	"strings"
	"testing"
)

// inboxListing adds Bob's and Carol's messages to INBOX and lists it.
func inboxListing(t *testing.T, e *env) ListMessagesOutput {
	t.Helper()
	e.imap.AddMessage(t, "INBOX", msgFromBob)
	e.imap.AddMessage(t, "INBOX", msgFromCarol)
	var l ListMessagesOutput
	e.callOK(t, "list_messages", map[string]any{"folder": "INBOX"}, &l)
	return l
}

func uidsOf(l ListMessagesOutput) []uint32 {
	uids := make([]uint32, 0, len(l.Messages))
	for _, m := range l.Messages {
		uids = append(uids, m.UID)
	}
	return uids
}

func TestMarkAndFlagTools(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	l := inboxListing(t, e)
	args := map[string]any{"folder": "INBOX", "uids": uidsOf(l), "uidvalidity": l.UIDValidity}

	var out ChangeOutput
	e.callOK(t, "mark_read", args, &out)
	if out.Changed != 2 || e.imap.Unseen(t, "INBOX") != 0 {
		t.Errorf("mark_read: changed %d, unseen %d", out.Changed, e.imap.Unseen(t, "INBOX"))
	}
	e.callOK(t, "mark_unread", args, &out)
	if e.imap.Unseen(t, "INBOX") != 2 {
		t.Errorf("mark_unread: unseen %d, want 2", e.imap.Unseen(t, "INBOX"))
	}

	e.callOK(t, "flag", args, &out)
	var after ListMessagesOutput
	e.callOK(t, "list_messages", map[string]any{"folder": "INBOX"}, &after)
	for _, m := range after.Messages {
		if !m.Flagged {
			t.Errorf("uid %d not flagged after flag", m.UID)
		}
	}
	e.callOK(t, "unflag", args, &out)
	e.callOK(t, "list_messages", map[string]any{"folder": "INBOX"}, &after)
	for _, m := range after.Messages {
		if m.Flagged {
			t.Errorf("uid %d still flagged after unflag", m.UID)
		}
	}
}

// A refused change says nothing was changed and how to recover.
func TestOrganizeToolErrors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	l := inboxListing(t, e)
	uids := uidsOf(l)
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"stale uidvalidity", map[string]any{"folder": "INBOX", "uids": uids, "uidvalidity": l.UIDValidity + 1}, "list it again"},
		{"missing uid", map[string]any{"folder": "INBOX", "uids": []uint32{uids[0], 999}, "uidvalidity": l.UIDValidity}, "nothing was changed"},
		{"no uidvalidity", map[string]any{"folder": "INBOX", "uids": uids}, "uidvalidity"},
		{"no uids", map[string]any{"folder": "INBOX", "uids": []uint32{}, "uidvalidity": l.UIDValidity}, "1 to 50"},
		{"no folder", map[string]any{"uids": uids, "uidvalidity": l.UIDValidity}, "folder"},
	}
	for _, tt := range tests {
		res := e.call(t, "mark_read", tt.args)
		if !res.IsError || !strings.Contains(text(res), tt.want) {
			t.Errorf("%s: IsError=%v text=%q, want an error mentioning %q", tt.name, res.IsError, text(res), tt.want)
		}
	}
	if n := e.imap.Unseen(t, "INBOX"); n != 2 {
		t.Errorf("unseen = %d, want 2: a refused call changed something", n)
	}
}
