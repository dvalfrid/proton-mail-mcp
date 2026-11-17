//go:build live

package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
	"github.com/dvalfrid/proton-mail-mcp/internal/send"
)

// TestLiveSendDraft SENDS ONE REAL EMAIL, from the owner to themselves,
// through their Bridge: create_draft, then send_draft, then it checks that
// the draft left Drafts and the message is in Sent (issue #181). Only with
// PROTON_LIVE_SEND=1, and only with the owner's go-ahead each time:
//
//	PROTON_LIVE_SEND=1 go test -tags live ./internal/server -run TestLiveSendDraft -v
//
// It prints uids, booleans and timings only.
func TestLiveSendDraft(t *testing.T) {
	if os.Getenv("PROTON_LIVE_SEND") != "1" {
		t.Skip("sends a real email to the owner; set PROTON_LIVE_SEND=1 only with their go-ahead")
	}
	cfg := liveConfig(t)
	tlsCfg, err := mail.TLSConfig(cfg.Host, cfg.CertFile)
	if err != nil {
		t.Fatal(err)
	}
	m := mail.New(mail.Options{Addr: cfg.IMAPAddr(), User: cfg.User, Password: cfg.Password, TLSConfig: tlsCfg, Logger: slog.New(slog.DiscardHandler)})
	t.Cleanup(func() { _ = m.Close() })
	// Sending is on for this one test, by the owner's go-ahead; the owner's
	// own setting is not read or changed.
	sender := send.New(send.Options{Allowed: true, Addr: cfg.SMTPAddr(), TLSConfig: tlsCfg, User: cfg.User, Password: cfg.Password})
	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()
	ss, err := New("live", m, Folders{Attachments: t.TempDir(), Outbox: t.TempDir()}, nil, nil, sender).Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "live-test", Version: "test"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	e := &env{session: cs}

	marker := fmt.Sprintf("pmm-send-test-%d", time.Now().Unix())
	var draft CreateDraftOutput
	e.callOK(t, "create_draft", map[string]any{
		"to": []string{cfg.User}, "subject": "proton-mail-mcp send test " + marker,
		"body": "A test of send_draft (issue #181), sent from this mailbox to itself. It can be deleted.",
	}, &draft)
	t.Logf("draft uid %d in Drafts", draft.UID)
	if draft.UID == 0 {
		t.Fatal("Bridge didn't report the draft's uid")
	}

	start := time.Now()
	var sent SendDraftOutput
	e.callOK(t, "send_draft", map[string]any{"uid": draft.UID, "uidvalidity": draft.UIDValidity, "recipients": []string{cfg.User}}, &sent)
	t.Logf("sent in %v, %d recipient(s)", time.Since(start).Round(time.Millisecond), len(sent.Recipients))

	// Bridge deletes the draft and files the sent copy through its event
	// stream, so give it time.
	var inDrafts, inSent bool
	for range 30 {
		time.Sleep(2 * time.Second)
		var d, s ListMessagesOutput
		e.callOK(t, "search_messages", map[string]any{"folder": "Drafts", "subject": marker}, &d)
		e.callOK(t, "search_messages", map[string]any{"folder": "Sent", "subject": marker}, &s)
		inDrafts, inSent = len(d.Messages) > 0, len(s.Messages) > 0
		if !inDrafts && inSent {
			break
		}
	}
	t.Logf("after %v: still in Drafts %v, in Sent %v", time.Since(start).Round(time.Second), inDrafts, inSent)
	if inDrafts {
		t.Error("the draft is still in Drafts")
	}
	if !inSent {
		t.Error("the message is not in Sent")
	}
}
