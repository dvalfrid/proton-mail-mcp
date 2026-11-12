package server

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// msgWithHostileName has one attachment whose name tries to leave the
// attachment folder.
const msgWithHostileName = "From: mallory@example.net\r\nTo: alice@example.com\r\nSubject: Report\r\n" +
	"Date: Mon, 02 Nov 2026 10:00:00 +0000\r\nMIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=b1\r\n\r\n" +
	"--b1\r\nContent-Type: text/plain\r\n\r\nSee attached.\r\n" +
	"--b1\r\nContent-Type: application/octet-stream\r\n" +
	"Content-Disposition: attachment; filename=\"..\\\\..\\\\..\\\\Startup\\\\run.bat\"\r\n\r\n" +
	"echo hello\r\n" +
	"--b1--\r\n"

// The attachment is saved, whole, into the attachment folder under a
// cleaned name; a second save keeps the first file. The message stays
// unread.
func TestSaveAttachment(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.imap.AddMessage(t, "INBOX", msgWithImage(t, "http://127.0.0.1:1/x.png"))
	e.imap.AddMessage(t, "INBOX", msgWithHostileName)
	l := listInbox(t, e)

	var out SaveAttachmentOutput
	e.callOK(t, "save_attachment", map[string]any{"folder": "INBOX", "uid": 1, "uidvalidity": l.UIDValidity, "attachment": 1}, &out)
	if out.FileName != "chart.png" || out.SavedTo != filepath.Join(e.dir, "chart.png") || out.Note != savedNote ||
		out.UntrustedContent == nil || out.UntrustedContent.Name != "chart.png" {
		t.Errorf("result = %+v", out)
	}
	data, err := os.ReadFile(out.SavedTo)
	if err != nil || !bytes.Equal(data, pngBytes(t, 40, 20)) || out.Size != int64(len(data)) {
		t.Errorf("saved %d bytes (%v), want the attachment's", len(data), err)
	}

	e.callOK(t, "save_attachment", map[string]any{"folder": "INBOX", "uid": 1, "uidvalidity": l.UIDValidity, "attachment": 1}, &out)
	if out.FileName != "chart (2).png" {
		t.Errorf("second save = %q, want a numbered name", out.FileName)
	}

	e.callOK(t, "save_attachment", map[string]any{"folder": "INBOX", "uid": 2, "uidvalidity": l.UIDValidity, "attachment": 1}, &out)
	if out.FileName != "run.bat" || filepath.Dir(out.SavedTo) != e.dir || out.UntrustedContent.Name != `..\..\..\Startup\run.bat` {
		t.Errorf("hostile name saved as %q in %q (sender's name %q)", out.FileName, filepath.Dir(out.SavedTo), out.UntrustedContent.Name)
	}
	entries, err := os.ReadDir(filepath.Dir(e.dir))
	if err != nil || len(entries) != 1 {
		t.Errorf("next to the attachment folder: %v (%v)", entries, err)
	}
	if n := e.imap.Unseen(t, "INBOX"); n != 2 {
		t.Errorf("unseen = %d after saving, want 2", n)
	}
}

func TestSaveAttachmentRefuses(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.imap.AddMessage(t, "INBOX", msgWithImage(t, "http://127.0.0.1:1/x.png"))
	l := listInbox(t, e)
	v := l.UIDValidity

	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"folder": "INBOX", "uid": 1, "attachment": 1}, `missing properties: ["uidvalidity"]`},
		{map[string]any{"folder": "INBOX", "uid": 1, "uidvalidity": 0, "attachment": 1}, "uid and uidvalidity are required"},
		{map[string]any{"folder": "INBOX", "uid": 1, "uidvalidity": v + 1, "attachment": 1}, "renumbered"},
		{map[string]any{"folder": "INBOX", "uid": 1, "uidvalidity": v, "attachment": 9}, "no attachment with that index"},
		{map[string]any{"folder": "INBOX", "uid": 9, "uidvalidity": v, "attachment": 1}, "message not found"},
		{map[string]any{"folder": "INBOX", "uid": 1, "uidvalidity": v, "attachment": 0}, "attachment is required"},
	} {
		res := e.call(t, "save_attachment", c.args)
		if !res.IsError || !strings.Contains(text(res), c.want) {
			t.Errorf("%v: IsError=%v %q, want %q", c.args, res.IsError, text(res), c.want)
		}
	}
	if _, err := os.Stat(e.dir); !os.IsNotExist(err) {
		t.Errorf("a refused save created the attachment folder: %v", err)
	}
}

// listInbox lists INBOX as it is, for its uidvalidity.
func listInbox(t *testing.T, e *env) ListMessagesOutput {
	t.Helper()
	var l ListMessagesOutput
	e.callOK(t, "list_messages", map[string]any{"folder": "INBOX"}, &l)
	return l
}
