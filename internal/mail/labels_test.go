package mail

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
)

// A message in INBOX and in two Labels/ mailboxes is listed with both
// labels; another message with the same Message-ID but other content is
// not taken for it (issue #172).
func TestListShowsLabels(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, srv, _ := newClient(t)
	srv.CreateMailbox(t, "Labels/Customer")
	at := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	labelled := "From: bob@example.org\nTo: alice@example.com\nSubject: Invoice 42\nMessage-ID: <42@example.org>\n\nPay.\n"
	plain := "From: bob@example.org\nTo: alice@example.com\nSubject: Lunch\nMessage-ID: <lunch@example.org>\n\nNoon.\n"
	for _, box := range []string{"INBOX", "Labels/Travel", "Labels/Customer"} {
		srv.AddMessageAt(t, box, labelled, at)
	}
	srv.AddMessageAt(t, "INBOX", plain, at.Add(time.Hour))
	// Same Message-ID, other content: a different message.
	srv.AddMessageAt(t, "Labels/Travel", strings.Replace(labelled, "Pay.", "Pay now, please.", 1), at)

	l, err := c.ListMessages(ctx, "INBOX", ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	for _, m := range l.Messages {
		got[m.Subject] = m.Labels
	}
	if !slices.Equal(got["Invoice 42"], []string{"Customer", "Travel"}) || len(got["Lunch"]) != 0 || l.LabelsIncomplete {
		t.Errorf("labels = %v, incomplete %v", got, l.LabelsIncomplete)
	}

	// search_messages gets them the same way.
	s, err := c.SearchMessages(ctx, "INBOX", SearchOptions{Subject: "Invoice"})
	if err != nil || len(s.Messages) != 1 || !slices.Equal(s.Messages[0].Labels, []string{"Customer", "Travel"}) {
		t.Errorf("search = %+v, %v", s, err)
	}
}

// A label mailbox is read once and kept until it changes: a second listing
// doesn't fetch it again, and a label added since shows up.
func TestLabelsAreCachedUntilChanged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, srv, _ := newClient(t)
	s := inbox(t, c, func(f string) {
		srv.AddMessage(t, f, message("bob@example.org", "Trip", "Mon, 02 Nov 2026 10:00:00 +0000"))
	}, 1)
	if n, err := c.AddLabel(ctx, s, "Labels/Travel"); err != nil || n != 1 {
		t.Fatalf("add label: %d, %v", n, err)
	}
	l, err := c.ListMessages(ctx, "INBOX", ListOptions{})
	if err != nil || len(l.Messages) != 1 || !slices.Equal(l.Messages[0].Labels, []string{"Travel"}) {
		t.Fatalf("first listing = %+v, %v", l, err)
	}
	opened := strings.Count(srv.Transcript(), `EXAMINE "Labels/Travel"`) + strings.Count(srv.Transcript(), "EXAMINE Labels/Travel")
	if _, err := c.ListMessages(ctx, "INBOX", ListOptions{}); err != nil {
		t.Fatal(err)
	}
	if again := strings.Count(srv.Transcript(), `EXAMINE "Labels/Travel"`) + strings.Count(srv.Transcript(), "EXAMINE Labels/Travel"); again != opened || opened == 0 {
		t.Errorf("Labels/Travel opened %d times, then %d: the unchanged label was read again", opened, again)
	}

	srv.CreateMailbox(t, "Labels/Work")
	if n, err := c.AddLabel(ctx, s, "Labels/Work"); err != nil || n != 1 {
		t.Fatalf("add second label: %d, %v", n, err)
	}
	l, err = c.ListMessages(ctx, "INBOX", ListOptions{})
	if err != nil || !slices.Equal(l.Messages[0].Labels, []string{"Travel", "Work"}) {
		t.Errorf("after adding a label = %+v, %v", l.Messages, err)
	}
}

// Out of time, labels are left out and the listing says so, rather than
// failing.
func TestLabelsOutOfTime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, srv, _ := newClient(t)
	srv.AddMessage(t, "INBOX", message("bob@example.org", "Trip", "Mon, 02 Nov 2026 10:00:00 +0000"))
	srv.AddMessage(t, "Labels/Travel", message("bob@example.org", "Other", "Mon, 02 Nov 2026 10:00:00 +0000"))
	err := c.do(ctx, func(ic *imapclient.Client) error {
		if _, err := ic.Select("INBOX", nil).Wait(); err != nil {
			return err
		}
		msgs, err := ic.Fetch(imapUIDAll(), fetchForLabels()).Collect()
		if err != nil {
			return err
		}
		labels, complete, err := c.labelsOf(ic, msgs, time.Now().Add(-time.Second))
		if err != nil || complete || len(labels) != 0 {
			t.Errorf("past the budget: %v, complete %v, %v", labels, complete, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
