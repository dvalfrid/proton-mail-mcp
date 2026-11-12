package mail

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// inbox adds n messages to INBOX and returns a Selection of all of them.
func inbox(t *testing.T, c *Client, add func(string), n int) Selection {
	t.Helper()
	for range n {
		add("INBOX")
	}
	l, err := c.ListMessages(context.Background(), "INBOX", ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	s := Selection{Folder: "INBOX", UIDValidity: l.UIDValidity}
	for _, m := range l.Messages {
		s.UIDs = append(s.UIDs, m.UID)
	}
	return s
}

func flagged(t *testing.T, c *Client) int {
	t.Helper()
	l, err := c.ListMessages(context.Background(), "INBOX", ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, m := range l.Messages {
		if m.Flagged {
			n++
		}
	}
	return n
}

func TestMarkReadAndFlag(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, srv, _ := newClient(t)
	s := inbox(t, c, func(f string) {
		srv.AddMessage(t, f, message("bob@example.org", "Hi", "Mon, 02 Nov 2026 10:00:00 +0000"))
	}, 3)

	if _, err := c.MarkRead(ctx, s, true); err != nil {
		t.Fatal(err)
	}
	if n := srv.Unseen(t, "INBOX"); n != 0 {
		t.Errorf("unseen = %d after mark read, want 0", n)
	}
	if _, err := c.MarkRead(ctx, Selection{Folder: "INBOX", UIDs: s.UIDs[:1], UIDValidity: s.UIDValidity}, false); err != nil {
		t.Fatal(err)
	}
	if n := srv.Unseen(t, "INBOX"); n != 1 {
		t.Errorf("unseen = %d after marking one unread, want 1", n)
	}

	if _, err := c.Flag(ctx, s, true); err != nil {
		t.Fatal(err)
	}
	if n := flagged(t, c); n != 3 {
		t.Errorf("flagged = %d, want 3", n)
	}
	if _, err := c.Flag(ctx, s, false); err != nil {
		t.Fatal(err)
	}
	if n := flagged(t, c); n != 0 {
		t.Errorf("flagged = %d after unflag, want 0", n)
	}

	if strings.Contains(srv.Transcript(), "EXPUNGE") || strings.Contains(srv.Transcript(), `\Deleted`) {
		t.Error("an organize call used EXPUNGE or \\Deleted")
	}
}

// Nothing changes unless every uid is valid and current.
func TestChangeRefusesBeforeTouching(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, srv, _ := newClient(t)
	s := inbox(t, c, func(f string) {
		srv.AddMessage(t, f, message("bob@example.org", "Hi", "Mon, 02 Nov 2026 10:00:00 +0000"))
	}, 2)
	many := make([]uint32, MaxChangeUIDs+1)
	for i := range many {
		many[i] = uint32(i + 1)
	}
	tests := []struct {
		name string
		sel  Selection
		want error
	}{
		{"stale uidvalidity", Selection{Folder: "INBOX", UIDs: s.UIDs, UIDValidity: s.UIDValidity + 1}, ErrUIDValidityChanged},
		{"one uid missing", Selection{Folder: "INBOX", UIDs: append(s.UIDs, 999), UIDValidity: s.UIDValidity}, ErrMessagesMissing},
		{"no uidvalidity", Selection{Folder: "INBOX", UIDs: s.UIDs}, ErrNoUIDValidity},
		{"no uids", Selection{Folder: "INBOX", UIDValidity: s.UIDValidity}, ErrNoUIDs},
		{"uid zero", Selection{Folder: "INBOX", UIDs: []uint32{0}, UIDValidity: s.UIDValidity}, ErrNoUIDs},
		{"too many", Selection{Folder: "INBOX", UIDs: many, UIDValidity: s.UIDValidity}, ErrTooManyUIDs},
	}
	for _, tt := range tests {
		if _, err := c.MarkRead(ctx, tt.sel, true); !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.want)
		}
	}
	if n := srv.Unseen(t, "INBOX"); n != 2 {
		t.Errorf("unseen = %d, want 2: a refused call changed something", n)
	}
	if strings.Contains(srv.Transcript(), "STORE") {
		t.Error("a refused call sent STORE")
	}
}
