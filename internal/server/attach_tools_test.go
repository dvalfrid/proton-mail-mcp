package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
)

// putInOutbox writes a file into the test's outbox folder, as the owner (or
// Claude, on their request) would.
func (e *env) putInOutbox(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.outbox, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A file in the outbox is attached to the draft: Drafts holds it under its
// name and size, and the result lists it for the owner (issue #173).
func TestCreateDraftWithAttachment(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	pdf := []byte("%PDF-1.7\n% an invented instruction\n")
	e.putInOutbox(t, "Guide.pdf", pdf)

	var out CreateDraftOutput
	e.callOK(t, "create_draft", map[string]any{
		"to": []string{"bob@example.org"}, "subject": "Guiden", "body": "Här är guiden.",
		"attachments": []string{"Guide.pdf"},
	}, &out)
	if len(out.Attachments) != 1 || out.Attachments[0] != (AttachedFile{Name: "Guide.pdf", Bytes: len(pdf)}) ||
		!strings.Contains(out.Note, "attached files") {
		t.Fatalf("create_draft = %+v", out)
	}

	var msg ReadMessageOutput
	e.callOK(t, "read_message", map[string]any{"folder": "Drafts", "uid": out.UID, "uidvalidity": out.UIDValidity}, &msg)
	m := msg.UntrustedContent
	if !strings.Contains(m.Body, "Här är guiden.") || len(m.Attachments) != 1 ||
		m.Attachments[0].Name != "Guide.pdf" || m.Attachments[0].Type != "application/pdf" || m.Attachments[0].Size != int64(len(pdf)) {
		t.Errorf("draft read back as %+v", m)
	}
}

// update_draft and create_reply_draft attach in the same way.
func TestUpdateAndReplyWithAttachment(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.putInOutbox(t, "plan.txt", []byte("the plan"))

	var created CreateDraftOutput
	e.callOK(t, "create_draft", map[string]any{"to": []string{"bob@example.org"}, "subject": "Plan"}, &created)
	var updated UpdateDraftOutput
	e.callOK(t, "update_draft", map[string]any{
		"uid": created.UID, "uidvalidity": created.UIDValidity, "to": []string{"bob@example.org"}, "subject": "Plan",
		"attachments": []string{"plan.txt"},
	}, &updated)
	if len(updated.Attachments) != 1 || updated.Attachments[0].Name != "plan.txt" {
		t.Errorf("update_draft = %+v", updated)
	}

	l := inboxListing(t, e)
	var reply CreateReplyDraftOutput
	e.callOK(t, "create_reply_draft", map[string]any{
		"folder": "INBOX", "uid": l.Messages[0].UID, "uidvalidity": l.UIDValidity, "body": "Se bilagan.",
		"attachments": []string{"plan.txt"},
	}, &reply)
	var msg ReadMessageOutput
	e.callOK(t, "read_message", map[string]any{"folder": "Drafts", "uid": reply.UID, "uidvalidity": reply.UIDValidity}, &msg)
	if a := msg.UntrustedContent.Attachments; len(reply.Attachments) != 1 || len(a) != 1 || a[0].Name != "plan.txt" {
		t.Errorf("create_reply_draft = %+v, read back %+v", reply, a)
	}
}

// Guardrail: a draft attaches nothing from outside the outbox folder, no
// hidden file and nothing missing; such a call is refused before anything
// reaches Bridge, and it uses none of the per-run limit.
func TestAttachRefusesOutsideTheOutbox(t *testing.T) {
	t.Parallel()
	e := newEnvWith(t, func(o *mail.Options) { o.MaxChanges = 1 })
	secret := filepath.Join(filepath.Dir(e.outbox), "secret.txt")
	if err := os.WriteFile(secret, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.putInOutbox(t, ".env", []byte("private"))

	for _, name := range []string{secret, "../secret.txt", `..\secret.txt`, ".env", "missing.pdf"} {
		res := e.call(t, "create_draft", map[string]any{"to": []string{"bob@example.org"}, "subject": "s", "attachments": []string{name}})
		if !res.IsError || !strings.Contains(text(res), "not attached") {
			t.Errorf("%q: IsError=%v %q", name, res.IsError, text(res))
		}
		if strings.Contains(text(res), "private") {
			t.Errorf("%q: the error shows the file's content", name)
		}
	}
	if e.imap.Sent("APPEND") {
		t.Error("a refused draft reached Bridge")
	}
	var out CreateDraftOutput
	e.callOK(t, "create_draft", map[string]any{"to": []string{"bob@example.org"}, "subject": "still allowed"}, &out)
}
