package server

import (
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"

	"github.com/dvalfrid/proton-mail-mcp/internal/config"
	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
	"github.com/dvalfrid/proton-mail-mcp/internal/send"
	"github.com/dvalfrid/proton-mail-mcp/internal/testutil"
)

// storedDraft is a draft as Bridge stores it, from the test account.
const storedDraft = "From: Alice <alice@example.com>\r\n" +
	"To: Bob <bob@example.org>\r\n" +
	"Bcc: carol@example.net\r\n" +
	"Subject: Lunch\r\n" +
	"Date: Mon, 16 Nov 2026 10:00:00 +0000\r\n" +
	"Message-ID: <d1@example.com>\r\n" +
	"References: <d1@protonmail.internalid>\r\n" +
	"X-Pm-Internal-Id: d1\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n" +
	"\r\n" +
	"Noon?\r\n"

type sendEnv struct {
	*env
	smtp *testutil.SMTPServer
}

func newSendEnv(t *testing.T, allowed bool) *sendEnv {
	t.Helper()
	smtpSrv := testutil.NewSMTPServer(t)
	tlsCfg, err := mail.TLSConfigFromPEM("127.0.0.1", smtpSrv.CertPEM())
	if err != nil {
		t.Fatal(err)
	}
	s := send.New(send.Options{Allowed: allowed, Addr: smtpSrv.Addr, TLSConfig: tlsCfg, User: testutil.User, Password: config.Secret(testutil.Password)})
	return &sendEnv{env: newEnvAll(t, nil, nil, s), smtp: smtpSrv}
}

// draftUID adds raw to Drafts and returns its uid and the folder's
// uidvalidity, as list_messages would give them.
func (e *sendEnv) draftUID(t *testing.T, folder, raw string) (uid, validity uint32) {
	t.Helper()
	e.imap.AddMessage(t, folder, raw, imap.FlagDraft)
	var out ListMessagesOutput
	e.callOK(t, "list_messages", map[string]any{"folder": folder}, &out)
	if len(out.Messages) == 0 {
		t.Fatal("no message listed")
	}
	return out.Messages[0].UID, out.UIDValidity
}

func TestSendDraft(t *testing.T) {
	t.Parallel()
	e := newSendEnv(t, true)
	uid, v := e.draftUID(t, "Drafts", storedDraft)
	var out SendDraftOutput
	e.callOK(t, "send_draft", map[string]any{"uid": uid, "uidvalidity": v, "recipients": []string{"bob@example.org", "carol@example.net"}}, &out)
	if out.From != "alice@example.com" || out.Subject != "Lunch" || len(out.Recipients) != 2 || out.Note == "" {
		t.Errorf("send_draft = %+v", out)
	}
	sent := e.smtp.Sent()
	if len(sent) != 1 || strings.Join(sent[0].Recipients, ",") != "bob@example.org,carol@example.net" || strings.Contains(string(sent[0].Data), "X-Pm-") {
		t.Fatalf("sent = %+v", sent)
	}
	if strings.Contains(e.logs.String(), "Lunch") || strings.Contains(e.logs.String(), "bob@example.org") {
		t.Error("the log names the subject or a recipient")
	}
}

// Every refusal sends nothing.
func TestSendDraftRefuses(t *testing.T) {
	t.Parallel()
	all := []string{"bob@example.org", "carol@example.net"}

	off := newSendEnv(t, false)
	uid, v := off.draftUID(t, "Drafts", storedDraft)
	res := off.call(t, "send_draft", map[string]any{"uid": uid, "uidvalidity": v, "recipients": all})
	if !res.IsError || !strings.Contains(text(res), "switched off") {
		t.Errorf("off: %s", text(res))
	}

	e := newSendEnv(t, true)
	uid, v = e.draftUID(t, "Drafts", storedDraft)
	inbox, iv := e.draftUID(t, "INBOX", strings.Replace(storedDraft, "d1@", "i1@", 2))
	notOwn := strings.Replace(strings.Replace(storedDraft, "Alice <alice@example.com>", "Mallory <mallory@example.com>", 1), "d1@", "m1@", 2)
	e.imap.AddMessage(t, "Drafts", notOwn, imap.FlagDraft)
	var drafts ListMessagesOutput
	e.callOK(t, "list_messages", map[string]any{"folder": "Drafts"}, &drafts)
	var mallory uint32
	for _, m := range drafts.Messages {
		if m.UID != uid {
			mallory = m.UID
		}
	}
	for name, c := range map[string]struct {
		args map[string]any
		says string
	}{
		"wrong recipients":    {map[string]any{"uid": uid, "uidvalidity": v, "recipients": []string{"bob@example.org"}}, "don't match"},
		"extra recipient":     {map[string]any{"uid": uid, "uidvalidity": v, "recipients": append([]string{"eve@example.com"}, all...)}, "don't match"},
		"stale uidvalidity":   {map[string]any{"uid": uid, "uidvalidity": v + 1, "recipients": all}, "uidvalidity"},
		"no uidvalidity":      {map[string]any{"uid": uid, "recipients": all}, "required"},
		"not in Drafts":       {map[string]any{"uid": uid + 100, "uidvalidity": v, "recipients": all}, "no draft"},
		"inbox uid as draft":  {map[string]any{"uid": inbox + 50, "uidvalidity": iv, "recipients": all}, ""},
		"someone else's From": {map[string]any{"uid": mallory, "uidvalidity": v, "recipients": all}, "nothing was sent"},
	} {
		res := e.call(t, "send_draft", c.args)
		if !res.IsError || !strings.Contains(text(res), c.says) {
			t.Errorf("%s: %s", name, text(res))
		}
	}
	if n := len(e.smtp.Sent()); n != 0 {
		t.Errorf("sent %d messages after refusals", n)
	}
}
