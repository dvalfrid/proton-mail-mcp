package mail

import (
	"context"
	"testing"
	"time"
)

// within_hours and since return only recent mail, newest first; older mail
// is left out even when the limit has room for it.
func TestListSince(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, srv, _ := newClient(t)
	now := time.Now()
	for _, m := range []struct {
		subject string
		age     time.Duration
	}{{"two hours", 2 * time.Hour}, {"twenty hours", 20 * time.Hour}, {"two days", 50 * time.Hour}, {"a month", 30 * 24 * time.Hour}} {
		at := now.Add(-m.age)
		srv.AddMessageAt(t, "INBOX", message("bob@example.org", m.subject, at.Format(time.RFC1123Z)), at)
	}
	subjects := func(l Listing) []string {
		var out []string
		for _, m := range l.Messages {
			out = append(out, m.Subject)
		}
		return out
	}

	l, err := c.ListMessages(ctx, "INBOX", ListOptions{Limit: 100, WithinHours: 24})
	if err != nil || len(l.Messages) != 2 || l.Messages[0].Subject != "two hours" || l.Messages[1].Subject != "twenty hours" {
		t.Errorf("within 24 hours = %v, %v", subjects(l), err)
	}
	l, err = c.ListMessages(ctx, "INBOX", ListOptions{Limit: 100, Since: now.AddDate(0, 0, -5)})
	if err != nil || len(l.Messages) != 3 {
		t.Errorf("since 5 days ago = %v, %v", subjects(l), err)
	}
	l, err = c.ListMessages(ctx, "INBOX", ListOptions{Limit: 100})
	if err != nil || len(l.Messages) != 4 {
		t.Errorf("no bound = %v, %v", subjects(l), err)
	}
}
