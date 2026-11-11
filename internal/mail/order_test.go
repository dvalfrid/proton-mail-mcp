package mail

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"
)

// Bridge doesn't number messages by date: its initial sync gives the newest
// messages the lowest uids (docs/bridge.md, section 3). "Newest first" must
// therefore mean by date, and the limit must apply after sorting.
func TestNewestFirstMeansByDate(t *testing.T) {
	t.Parallel()
	c, srv, _ := newClient(t)
	// Appended out of date order, as after a Bridge sync: uid → day of November.
	days := []int{5, 2, 4, 1, 3} // uid 1 is 5 Nov, uid 2 is 2 Nov, …
	for i, day := range days {
		srv.AddMessage(t, "INBOX", fmt.Sprintf("From: bob@example.org\nTo: alice@example.com\nSubject: Day %d\n"+
			"Date: %s, %02d Nov 2026 10:00:00 +0000\n\nHello %d.\n", day, weekday(day), day, i+1))
	}

	uidsOf := func(s []Summary) []uint32 {
		out := []uint32{}
		for _, m := range s {
			out = append(out, m.UID)
		}
		return out
	}
	ctx := context.Background()

	got, err := c.ListMessages(ctx, "INBOX", ListOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint32{1, 3, 5}; !slices.Equal(uidsOf(got), want) { // 5, 4 and 3 Nov
		t.Errorf("list: uids = %v, want %v (newest by date first)", uidsOf(got), want)
	}

	got, err = c.SearchMessages(ctx, "INBOX", SearchOptions{From: "bob", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint32{1, 3}; !slices.Equal(uidsOf(got), want) {
		t.Errorf("search: uids = %v, want %v (newest by date first)", uidsOf(got), want)
	}
}

// The sort date is the earlier of the received date and the Date header.
// Bridge sometimes gives a synced message the sync time as its received
// date (seen on a real Bridge): such a message must not jump to the top.
// And a sender who writes a future Date header must not either.
func TestSortDateIsTheEarlierOfReceivedAndHeader(t *testing.T) {
	t.Parallel()
	c, srv, _ := newClient(t)
	msg := func(subject, date string) string {
		return fmt.Sprintf("From: bob@example.org\nSubject: %s\nDate: %s\n\nHi.\n", subject, date)
	}
	at := func(s string) time.Time {
		d, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	srv.AddMessage(t, "INBOX", msg("Normal, 3 Nov", "Tue, 03 Nov 2026 10:00:00 +0000"))                                   // uid 1
	srv.AddMessageAt(t, "INBOX", msg("Old, synced late", "Mon, 03 Feb 2020 10:00:00 +0000"), at("2026-11-11T21:33:00Z"))  // uid 2
	srv.AddMessageAt(t, "INBOX", msg("Claims the future", "Fri, 01 Jan 2027 10:00:00 +0000"), at("2026-11-01T10:00:00Z")) // uid 3
	srv.AddMessage(t, "INBOX", msg("Normal, 5 Nov", "Thu, 05 Nov 2026 10:00:00 +0000"))                                   // uid 4

	got, err := c.ListMessages(context.Background(), "INBOX", ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for _, m := range got {
		subjects = append(subjects, m.Subject)
	}
	want := []string{"Normal, 5 Nov", "Normal, 3 Nov", "Claims the future", "Old, synced late"}
	if !slices.Equal(subjects, want) {
		t.Errorf("order = %q, want %q", subjects, want)
	}
}

// Same date: the higher uid comes first, so the order is stable.
func TestSameDateOrdersByUID(t *testing.T) {
	t.Parallel()
	c, srv, _ := newClient(t)
	for i := range 3 {
		srv.AddMessage(t, "INBOX", fmt.Sprintf("From: bob@example.org\nSubject: Same %d\nDate: Mon, 02 Nov 2026 10:00:00 +0000\n\nHi.\n", i))
	}
	got, err := c.ListMessages(context.Background(), "INBOX", ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].UID != 3 || got[2].UID != 1 {
		t.Errorf("got %+v, want uids 3, 2, 1", got)
	}
}

// weekday returns the RFC 5322 day name of a day in November 2026, which
// starts on a Sunday.
func weekday(day int) string {
	return []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}[(day-1)%7]
}
