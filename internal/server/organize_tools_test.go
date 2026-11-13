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

// labelListing puts the two messages in Labels/Travel, as Bridge lists a
// labelled message there too, and lists the label.
func labelListing(t *testing.T, e *env) ListMessagesOutput {
	t.Helper()
	e.imap.AddMessage(t, "Labels/Travel", msgFromBob)
	e.imap.AddMessage(t, "Labels/Travel", msgFromCarol)
	var l ListMessagesOutput
	e.callOK(t, "list_messages", map[string]any{"folder": "Labels/Travel"}, &l)
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

func TestMoveAndLabelTools(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	l := inboxListing(t, e)
	common := map[string]any{"folder": "INBOX", "uids": uidsOf(l), "uidvalidity": l.UIDValidity}

	var out ChangeOutput
	e.callOK(t, "add_label", organizeArgs("add_label", common), &out)
	var labelled ListMessagesOutput
	e.callOK(t, "list_messages", map[string]any{"folder": "Labels/Travel"}, &labelled)
	if out.Changed != 2 || len(labelled.Messages) != 2 {
		t.Errorf("add_label: changed %d, %d under the label", out.Changed, len(labelled.Messages))
	}

	e.callOK(t, "move_message", map[string]any{"folder": "INBOX", "uids": uidsOf(l), "uidvalidity": l.UIDValidity, "to": "Folders/Work"}, &out)
	var inbox, work ListMessagesOutput
	e.callOK(t, "list_messages", map[string]any{"folder": "INBOX"}, &inbox)
	e.callOK(t, "list_messages", map[string]any{"folder": "Folders/Work"}, &work)
	if len(inbox.Messages) != 0 || len(work.Messages) != 2 {
		t.Errorf("move: %d left in INBOX, %d in Folders/Work", len(inbox.Messages), len(work.Messages))
	}
}

// A refused move says why and which tool to use.
func TestMoveToolRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	l := inboxListing(t, e)
	for to, want := range map[string]string{"Trash": "trash_message", "Labels/Travel": "add_label", "Drafts": "can't be moved to", "Folders/Nope": "no such folder", "": "to is required"} {
		res := e.call(t, "move_message", map[string]any{"folder": "INBOX", "uids": uidsOf(l), "uidvalidity": l.UIDValidity, "to": to})
		if !res.IsError || !strings.Contains(text(res), want) {
			t.Errorf("to %q: IsError=%v text=%q, want %q", to, res.IsError, text(res), want)
		}
	}
	res := e.call(t, "add_label", map[string]any{"folder": "INBOX", "uids": uidsOf(l), "uidvalidity": l.UIDValidity, "label": "Archive"})
	if !res.IsError || !strings.Contains(text(res), "not a label") {
		t.Errorf("add_label to Archive: IsError=%v text=%q", res.IsError, text(res))
	}
}

func TestTrashTool(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	l := inboxListing(t, e)
	var out ChangeOutput
	e.callOK(t, "trash_message", map[string]any{"folder": "INBOX", "uids": uidsOf(l), "uidvalidity": l.UIDValidity}, &out)
	var trash ListMessagesOutput
	e.callOK(t, "list_messages", map[string]any{"folder": "Trash"}, &trash)
	if out.Changed != 2 || len(trash.Messages) != 2 {
		t.Errorf("trash: changed %d, %d in Trash", out.Changed, len(trash.Messages))
	}
	res := e.call(t, "trash_message", map[string]any{"folder": "Trash", "uids": uidsOf(trash), "uidvalidity": trash.UIDValidity})
	if !res.IsError || !strings.Contains(text(res), "already in Trash") {
		t.Errorf("trash from Trash: IsError=%v text=%q", res.IsError, text(res))
	}
}

// remove_label takes the label off the given messages only: they leave
// Labels/Travel, the rest stay, and the copy in INBOX is untouched.
func TestRemoveLabel(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	inbox := inboxListing(t, e)
	l := labelListing(t, e)
	var out ChangeOutput
	e.callOK(t, "remove_label", map[string]any{"folder": "Labels/Travel", "uids": uidsOf(l)[:1], "uidvalidity": l.UIDValidity}, &out)
	if out.Changed != 1 || out.Folder != "Labels/Travel" {
		t.Errorf("remove_label = %+v", out)
	}
	var after ListMessagesOutput
	e.callOK(t, "list_messages", map[string]any{"folder": "Labels/Travel"}, &after)
	if len(after.Messages) != 1 || after.Messages[0].UID != uidsOf(l)[1] {
		t.Errorf("Labels/Travel after = %+v", after.Messages)
	}
	e.callOK(t, "list_messages", map[string]any{"folder": "INBOX"}, &after)
	if len(after.Messages) != len(inbox.Messages) {
		t.Errorf("INBOX has %d messages, want %d", len(after.Messages), len(inbox.Messages))
	}
	res := e.call(t, "remove_label", map[string]any{"folder": "Labels/Travel", "uids": uidsOf(l)[:1], "uidvalidity": l.UIDValidity})
	if !res.IsError || !strings.Contains(text(res), "nothing was changed") {
		t.Errorf("removing it again: IsError=%v %q", res.IsError, text(res))
	}
}
