package mail

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// The owner's addresses are the account's and the senders in Sent. A draft
// from one of them is written From it; any other is refused, and the error
// names the known ones (issue #162).
func TestDraftFromOwnAddress(t *testing.T) {
	t.Parallel()
	c, srv, _ := newClient(t)
	ctx := context.Background()
	srv.AddMessage(t, "Sent", "From: Alice <alice@example.org>\r\nTo: bob@example.net\r\nSubject: s\r\n\r\nx\r\n")

	for _, from := range []string{"alice@example.org", "ALICE@example.org", "Alice <alice@example.com>"} {
		if _, err := c.CreateDraft(ctx, Draft{From: from, To: []string{"bob@example.net"}, Subject: "hi"}); err != nil {
			t.Errorf("from %q: %v", from, err)
		}
	}
	_, err := c.CreateDraft(ctx, Draft{From: "mallory@example.net", To: []string{"bob@example.net"}})
	if !errors.Is(err, ErrNotOwnAddress) || !errors.Is(err, ErrBadDraft) || !strings.Contains(err.Error(), "alice@example.org") {
		t.Errorf("from a stranger: %v", err)
	}
	if !strings.Contains(srv.Transcript(), "From: <alice@example.org>") {
		t.Error("no draft was written From the alias")
	}

	// An address first used after the list was read is found on a second look.
	srv.AddMessage(t, "Sent", "From: info@example.org\r\nTo: bob@example.net\r\nSubject: s\r\n\r\nx\r\n")
	if got, err := c.OwnAddress(ctx, "info@example.org"); err != nil || got != "info@example.org" {
		t.Errorf("new alias: %q, %v", got, err)
	}
}

// A reply comes from the owner's address the mail was sent to, and
// reply-all leaves out every address of the owner's (issue #162).
func TestReplyFromTheAddressItCameTo(t *testing.T) {
	t.Parallel()
	c := New(Options{User: "alice@example.com", Location: time.UTC})
	own := map[string]string{"alice@example.com": "alice@example.com", "info@example.org": "info@example.org"}
	in := "From: Bob <bob@example.net>\r\nTo: Info <info@example.org>\r\nCc: carol@example.net, alice@example.com\r\n" +
		"Subject: Q\r\n\r\nA question.\r\n"
	d, err := c.reply([]byte(in), Reply{Body: "ok", All: true}, own, false)
	if err != nil {
		t.Fatal(err)
	}
	if d.From != "info@example.org" || !slices.Equal(d.To, []string{`"Bob" <bob@example.net>`}) || !slices.Equal(d.Cc, []string{"<carol@example.net>"}) {
		t.Errorf("from %q to %q cc %q", d.From, d.To, d.Cc)
	}

	// The owner's own mail from an alias, found outside Sent (All Mail):
	// it goes to its recipients, from that alias.
	mine := "From: info@example.org\r\nTo: bob@example.net\r\nSubject: Plan\r\n\r\nThe plan.\r\n"
	d, err = c.reply([]byte(mine), Reply{Body: "ok"}, own, false)
	if err != nil || d.From != "info@example.org" || !slices.Equal(d.To, []string{"<bob@example.net>"}) {
		t.Errorf("own mail outside Sent: from %q to %q, %v", d.From, d.To, err)
	}

	// Sent to none of the owner's known addresses: from the account.
	d, err = c.reply([]byte("From: bob@example.net\r\nTo: list@example.net\r\nSubject: x\r\n\r\ny\r\n"), Reply{Body: "ok"}, own, false)
	if err != nil || d.From != "" {
		t.Errorf("unknown recipient: from %q, %v", d.From, err)
	}
}
