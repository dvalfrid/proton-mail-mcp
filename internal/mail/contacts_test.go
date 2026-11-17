package mail

import (
	"context"
	"strings"
	"testing"
)

// People come from Sent (whom the owner wrote to) and INBOX (who wrote),
// matched on name or address without case or accents, ranked with those
// the owner wrote to first; the owner's own addresses never (issue #187).
func TestFindContacts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, srv, _ := newClient(t)
	srv.AddMessage(t, "Sent", "From: alice@example.com\r\nTo: Åsa Berg <asa@example.org>\r\nCc: Bob <bob@example.org>\r\nDate: Mon, 02 Nov 2026 10:00:00 +0000\r\nSubject: s\r\n\r\nx\r\n")
	srv.AddMessage(t, "Sent", "From: alice@example.com\r\nTo: asa@example.org\r\nBcc: alice@example.com\r\nDate: Tue, 03 Nov 2026 10:00:00 +0000\r\nSubject: s\r\n\r\nx\r\n")
	for range 3 {
		srv.AddMessage(t, "INBOX", "From: Åsa Spam <asa.spam@example.net>\r\nTo: alice@example.com\r\nDate: Wed, 04 Nov 2026 10:00:00 +0000\r\nSubject: s\r\n\r\nx\r\n")
	}
	srv.AddMessage(t, "INBOX", "From: Åsa Berg <ASA@example.org>\r\nTo: alice@example.com\r\nDate: Thu, 05 Nov 2026 10:00:00 +0000\r\nSubject: s\r\n\r\nx\r\n")

	got, err := c.FindContacts(ctx, "asa")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.People) != 2 {
		t.Fatalf("people = %+v", got.People)
	}
	berg, spam := got.People[0], got.People[1]
	// Written to twice, so first, though the other sent more.
	if berg.Address != "asa@example.org" || berg.Name != "Åsa Berg" || berg.Sent != 2 || berg.Received != 1 {
		t.Errorf("first = %+v", berg)
	}
	if spam.Address != "asa.spam@example.net" || spam.Sent != 0 || spam.Received != 3 {
		t.Errorf("second = %+v", spam)
	}
	if got.Scanned["Sent"] != 2 || got.Scanned["INBOX"] != 4 {
		t.Errorf("scanned = %v", got.Scanned)
	}

	for _, q := range []string{"alice", "example.com"} {
		if got, err := c.FindContacts(ctx, q); err != nil || len(got.People) != 0 {
			t.Errorf("%q found the owner: %+v, %v", q, got.People, err)
		}
	}
	if got, err := c.FindContacts(ctx, "BOB"); err != nil || len(got.People) != 1 || got.People[0].Sent != 1 {
		t.Errorf("bob = %+v, %v", got.People, err)
	}
	if _, err := c.FindContacts(ctx, "  "); err == nil {
		t.Error("an empty query was accepted")
	}
	if n := srv.Unseen(t, "INBOX"); n != 4 {
		t.Errorf("unseen = %d: reading addresses marked mail read", n)
	}
}

// Words match in any order, Archive is read too, and the owner's own mail
// in Archive counts as written to.
func TestFindContactsWordsAndArchive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, srv, _ := newClient(t)
	srv.AddMessage(t, "Archive", "From: Anna Lind <anna@example.org>\r\nTo: alice@example.com\r\nDate: Mon, 02 Nov 2026 10:00:00 +0000\r\nSubject: s\r\n\r\nx\r\n")
	srv.AddMessage(t, "Archive", "From: alice@example.com\r\nTo: Per Holm <per@example.net>\r\nDate: Mon, 02 Nov 2026 11:00:00 +0000\r\nSubject: s\r\n\r\nx\r\n")

	for _, q := range []string{"lind anna", "Anna  LIND", "anna example.org"} {
		got, err := c.FindContacts(ctx, q)
		if err != nil || len(got.People) != 1 || got.People[0].Address != "anna@example.org" || got.People[0].Received != 1 {
			t.Errorf("%q = %+v, %v", q, got.People, err)
		}
	}
	if got, _ := c.FindContacts(ctx, "anna holm"); len(got.People) != 0 {
		t.Errorf("words from two people matched one: %+v", got.People)
	}
	got, err := c.FindContacts(ctx, "holm per")
	if err != nil || len(got.People) != 1 || got.People[0].Sent != 1 || got.Scanned["Archive"] != 2 {
		t.Errorf("own mail in Archive = %+v, %v", got, err)
	}
}

// A folder is read once and kept until it changes: later searches ask
// STATUS only, and a new message is seen.
func TestFindContactsCached(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, srv, _ := newClient(t)
	srv.AddMessage(t, "INBOX", "From: Bob <bob@example.org>\r\nTo: alice@example.com\r\nDate: Mon, 02 Nov 2026 10:00:00 +0000\r\nSubject: s\r\n\r\nx\r\n")
	opened := func() int {
		return strings.Count(srv.Transcript(), "EXAMINE INBOX") + strings.Count(srv.Transcript(), `EXAMINE "INBOX"`)
	}

	if got, err := c.FindContacts(ctx, "bob"); err != nil || len(got.People) != 1 {
		t.Fatalf("first = %+v, %v", got, err)
	}
	first := opened()
	if _, err := c.FindContacts(ctx, "carol"); err != nil {
		t.Fatal(err)
	}
	if opened() != first || first == 0 {
		t.Errorf("INBOX opened %d times, then %d: the unchanged folder was read again", first, opened())
	}

	srv.AddMessage(t, "INBOX", "From: Carol <carol@example.net>\r\nTo: alice@example.com\r\nDate: Tue, 03 Nov 2026 10:00:00 +0000\r\nSubject: s\r\n\r\nx\r\n")
	if got, err := c.FindContacts(ctx, "carol"); err != nil || len(got.People) != 1 {
		t.Errorf("after a new message = %+v, %v", got, err)
	}
}
