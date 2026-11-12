package mail

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// A message larger than the fetch cap is fetched only up to the cap, parsed
// from that prefix and marked truncated. The cap is lowered for the test;
// in production it is MaxMessageBytes.
func TestReadMessageOverFetchCap(t *testing.T) {
	t.Parallel()
	c, srv, _ := newClient(t)
	c.maxFetch = 2048

	body := strings.Repeat("All work and no play makes a dull message.\n", 250) // ~11 KB
	srv.AddMessage(t, "INBOX", "From: bob@example.org\nTo: alice@example.com\nSubject: Long\n"+
		"Date: Mon, 02 Nov 2026 10:00:00 +0000\nContent-Type: text/plain; charset=utf-8\n\n"+body)

	m, err := c.ReadMessage(context.Background(), "INBOX", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Truncated {
		t.Error("truncated = false for a message over the fetch cap")
	}
	if m.Subject != "Long" {
		t.Errorf("subject = %q: the headers should survive the cut", m.Subject)
	}
	if n := len(m.Body); n == 0 || n > 2048 {
		t.Errorf("body length = %d, want 1..2048", n)
	}
	if !strings.Contains(srv.Transcript(), "BODY.PEEK[]<0.2048>") {
		t.Error("the fetch did not ask for at most the cap")
	}
	if n := srv.Unseen(t, "INBOX"); n != 1 {
		t.Errorf("unseen = %d, want 1", n)
	}
}

// Listing a folder of a few thousand messages stays correct and well within
// the operation timeout.
func TestListLargeFolder(t *testing.T) {
	t.Parallel()
	c, srv, _ := newClient(t)
	const n = 3000
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range n {
		date := start.Add(time.Duration(i) * time.Hour).Format(time.RFC1123Z)
		srv.AddMessage(t, "INBOX", fmt.Sprintf("From: bob@example.org\nTo: alice@example.com\nSubject: Message %d\nDate: %s\n\nHello.\n", i+1, date))
	}

	began := time.Now()
	got, err := messages(c.ListMessages(context.Background(), "INBOX", ListOptions{Limit: MaxListLimit, UnreadOnly: true}))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("listed %d of %d messages in %v", len(got), n, time.Since(began))

	if len(got) != MaxListLimit {
		t.Fatalf("got %d messages, want %d", len(got), MaxListLimit)
	}
	if got[0].Subject != fmt.Sprintf("Message %d", n) || got[len(got)-1].Subject != fmt.Sprintf("Message %d", n-MaxListLimit+1) {
		t.Errorf("first = %q, last = %q: want the %d newest, newest first", got[0].Subject, got[len(got)-1].Subject, MaxListLimit)
	}
	if u := srv.Unseen(t, "INBOX"); u != n {
		t.Errorf("unseen = %d after listing, want %d", u, n)
	}
}

// An attachment of a message larger than the fetch cap is refused, not
// returned cut as if it were whole.
func TestAttachmentOverFetchCap(t *testing.T) {
	t.Parallel()
	c, srv, _ := newClient(t)
	c.maxFetch = 100
	srv.AddMessage(t, "INBOX", msgWithParts)

	if _, _, err := c.Attachment(context.Background(), "INBOX", 1, 0, 1, 1<<20); !errors.Is(err, ErrMessageTooLarge) {
		t.Errorf("err = %v, want ErrMessageTooLarge", err)
	}
}

// An attachment whose encoding breaks off is refused as damaged.
func TestAttachmentDamaged(t *testing.T) {
	t.Parallel()
	raw := strings.Replace(msgWithParts, "aW1hZ2UgYnl0ZXM=", "aW1hZ2Ug!!!", 1)
	if _, _, err := ExtractAttachment(strings.NewReader(raw), 2, 1<<20); !errors.Is(err, ErrAttachmentDamaged) {
		t.Errorf("err = %v, want ErrAttachmentDamaged", err)
	}
}
