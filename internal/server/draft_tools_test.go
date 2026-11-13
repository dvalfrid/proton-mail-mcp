package server

import (
	"strings"
	"testing"

	"github.com/dvalfrid/proton-mail-mcp/internal/help"
	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
	"github.com/dvalfrid/proton-mail-mcp/internal/testutil"
)

// A draft lands in Drafts, read back as written, with its uid returned.
func TestCreateDraft(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	var out CreateDraftOutput
	e.callOK(t, "create_draft", map[string]any{
		"to":      []string{"Åsa Example <asa@example.com>"},
		"cc":      []string{"bob@example.org"},
		"bcc":     []string{"carol@example.net"},
		"subject": "Mötet på fredag",
		"body":    "Hej Åsa,\n\nkan vi flytta mötet till 14.00?\n\n/D",
	}, &out)
	if out.Folder != "Drafts" || out.UID == 0 || out.UIDValidity == 0 || !strings.Contains(out.Note, "not sent") {
		t.Fatalf("create_draft = %+v", out)
	}

	var msg ReadMessageOutput
	e.callOK(t, "read_message", map[string]any{"folder": "Drafts", "uid": out.UID, "uidvalidity": out.UIDValidity}, &msg)
	m := msg.UntrustedContent
	if m.Subject != "Mötet på fredag" || !strings.Contains(m.Body, "kan vi flytta mötet till 14.00?") ||
		len(m.To) != 1 || !strings.Contains(m.To[0], "asa@example.com") || len(m.Cc) != 1 || m.MessageID == "" {
		t.Errorf("draft read back as %+v", m)
	}
	if !strings.Contains(m.From[0], testutil.User) {
		t.Errorf("From = %v, want the account address", m.From)
	}
	for _, bad := range []string{"EXPUNGE", `\Deleted`} {
		if e.imap.Sent(bad) {
			t.Errorf("create_draft sent a command containing %q", bad)
		}
	}
}

// A draft that could add headers or hidden recipients, or is absurd, is
// refused before anything reaches Bridge, and the error doesn't repeat the
// address (it may come from an email).
func TestCreateDraftRefuses(t *testing.T) {
	t.Parallel()
	many := make([]string, mail.MaxDraftRecipients+1)
	for i := range many {
		many[i] = "x@example.com"
	}
	for name, args := range map[string]map[string]any{
		"no to":            {"to": []string{}, "subject": "s", "body": "b"},
		"header in to":     {"to": []string{"a@example.com\r\nBcc: evil@example.net"}, "subject": "s"},
		"header in subj":   {"to": []string{"a@example.com"}, "subject": "hi\r\nBcc: evil@example.net"},
		"list in one":      {"to": []string{"a@example.com, evil@example.net"}, "subject": "s"},
		"not an address":   {"to": []string{"evil-but-no-at"}, "subject": "s"},
		"too many":         {"to": many, "subject": "s"},
		"subject too long": {"to": []string{"a@example.com"}, "subject": strings.Repeat("x", mail.MaxDraftSubjectRunes+1)},
		"body too large":   {"to": []string{"a@example.com"}, "body": strings.Repeat("x", mail.MaxDraftBodyBytes+1)},
	} {
		e := newEnv(t)
		res := e.call(t, "create_draft", args)
		if !res.IsError || !strings.Contains(text(res), "the draft was not created") {
			t.Errorf("%s: IsError=%v %q", name, res.IsError, text(res))
		}
		if strings.Contains(text(res), "evil") {
			t.Errorf("%s: the error repeats the address: %q", name, text(res))
		}
		if e.imap.Sent("APPEND") {
			t.Errorf("%s: APPEND was sent", name)
		}
	}
}

// Drafts count toward the per-run change limit, so an email can't make
// Claude fill Drafts.
func TestCreateDraftCountsTowardTheLimit(t *testing.T) {
	t.Parallel()
	e := newEnvWith(t, func(o *mail.Options) { o.MaxChanges = 1 })
	args := map[string]any{"to": []string{"a@example.com"}, "subject": "s", "body": "b"}
	var out CreateDraftOutput
	e.callOK(t, "create_draft", args, &out)
	res := e.call(t, "create_draft", args)
	if !res.IsError || !strings.Contains(text(res), "PROTON_MAX_CHANGES") || !strings.Contains(text(res), "Help: "+help.URL(help.ChangeLimit)) {
		t.Errorf("second draft past the limit: IsError=%v %q", res.IsError, text(res))
	}
}
