package server

import (
	"encoding/base64"
	"strings"
	"testing"
)

// msgWithReport is a message from Bob with an attached PDF and an inline
// image in its HTML.
const msgWithReport = "From: Bob Example <bob@example.org>\r\nTo: alice@example.com\r\nCc: carol@example.net\r\nSubject: Report\r\n" +
	"Date: Mon, 02 Nov 2026 10:00:00 +0000\r\nMessage-ID: <r1@example.org>\r\nMIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=m1\r\n\r\n" +
	"--m1\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nHere is the report.\r\n" +
	"--m1\r\nContent-Type: image/png\r\nContent-Disposition: inline; filename=logo.png\r\nContent-Transfer-Encoding: base64\r\n\r\naGVsbG8=\r\n" +
	"--m1\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=report.pdf\r\nContent-Transfer-Encoding: base64\r\n\r\n"

func reportMessage() string {
	return msgWithReport + base64.StdEncoding.EncodeToString([]byte("%PDF-1.4 a test report")) + "\r\n--m1--\r\n"
}

func TestCreateForwardDraft(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.imap.AddMessage(t, "INBOX", reportMessage())
	l := listInbox(t, e)

	var out CreateForwardDraftOutput
	e.callOK(t, "create_forward_draft", map[string]any{
		"folder": "INBOX", "uid": l.Messages[0].UID, "uidvalidity": l.UIDValidity,
		"to": []string{"Dave <dave@example.com>"}, "body": "FYI, see below.",
	}, &out)
	if out.UID == 0 || out.Subject != "Fwd: Report" || out.From != "alice@example.com" ||
		len(out.Forwarded) != 1 || out.Forwarded[0] != "report.pdf" || len(out.LeftOut) != 0 || !strings.Contains(out.Note, "not sent") {
		t.Fatalf("create_forward_draft = %+v", out)
	}

	var msg ReadMessageOutput
	e.callOK(t, "read_message", map[string]any{"folder": "Drafts", "uid": out.UID, "uidvalidity": out.UIDValidity}, &msg)
	d := msg.UntrustedContent
	for _, want := range []string{"FYI, see below.", "---------- Forwarded message ----------", "From: Bob Example <bob@example.org>", "Subject: Report", "Cc: carol@example.net", "Here is the report."} {
		if !strings.Contains(d.Body, want) {
			t.Errorf("forward body lacks %q:\n%s", want, d.Body)
		}
	}
	if len(d.To) != 1 || !strings.Contains(d.To[0], "dave@example.com") || len(d.Cc) != 0 {
		t.Errorf("recipients To %v Cc %v: only the owner's, never the original's", d.To, d.Cc)
	}
	if len(d.Attachments) != 1 || d.Attachments[0].Name != "report.pdf" || d.Attachments[0].Type != "application/pdf" || d.Attachments[0].Size != int64(len("%PDF-1.4 a test report")) {
		t.Errorf("draft attachments %+v", d.Attachments)
	}
	if n := e.imap.Unseen(t, "INBOX"); n != 1 {
		t.Errorf("unseen in INBOX = %d, want 1: the original was marked read", n)
	}
}

func TestCreateForwardDraftOptions(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.imap.AddMessage(t, "INBOX", reportMessage())
	l := listInbox(t, e)
	base := map[string]any{"folder": "INBOX", "uid": l.Messages[0].UID, "uidvalidity": l.UIDValidity}
	with := func(kv ...any) map[string]any {
		m := map[string]any{}
		for k, v := range base {
			m[k] = v
		}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1] //nolint:forcetypeassert // keys are strings in this test
		}
		return m
	}

	var out CreateForwardDraftOutput
	e.callOK(t, "create_forward_draft", with("to", []string{"dave@example.com"}, "without_attachments", true), &out)
	if len(out.Forwarded) != 0 || len(out.LeftOut) != 1 || !strings.Contains(out.LeftOut[0], "as asked") {
		t.Errorf("without attachments: %+v", out)
	}

	for name, c := range map[string]struct {
		args map[string]any
		says string
	}{
		"no recipients":     {with("to", []string{}), "recipient"},
		"stale uidvalidity": {with("to", []string{"dave@example.com"}, "uidvalidity", l.UIDValidity+1), "renumbered"},
		"no folder":         {map[string]any{"folder": "", "uid": 1, "uidvalidity": l.UIDValidity, "to": []string{"dave@example.com"}}, "folder is required"},
		"stranger's from":   {with("to", []string{"dave@example.com"}, "from", "mallory@example.com"), "mallory@example.com"},
	} {
		res := e.call(t, "create_forward_draft", c.args)
		if !res.IsError || !strings.Contains(text(res), c.says) {
			t.Errorf("%s: %s", name, text(res))
		}
	}
}
