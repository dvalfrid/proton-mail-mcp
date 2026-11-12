package mail

import (
	"context"
	"slices"
	"testing"
	"time"
)

// Search days are the owner's days and dates are shown in the owner's time
// zone, wherever that is: Bridge itself only knows UTC days.
func TestDaysAreTheOwnersDays(t *testing.T) {
	t.Parallel()
	c, srv, _ := newClient(t)
	// The received date follows the Date header (AddMessage).
	srv.AddMessage(t, "INBOX", message("bob@example.org", "Late on the 8th, UTC", "Sun, 08 Nov 2026 23:30:00 +0000"))   // uid 1
	srv.AddMessage(t, "INBOX", message("bob@example.org", "Midday on the 9th", "Mon, 09 Nov 2026 12:00:00 +0000"))      // uid 2
	srv.AddMessage(t, "INBOX", message("bob@example.org", "Early on the 10th, UTC", "Tue, 10 Nov 2026 03:00:00 +0000")) // uid 3

	ninth := SearchOptions{Since: day("2026-11-09"), Before: day("2026-11-10")}
	for _, tt := range []struct {
		loc  *time.Location
		want []uint32
	}{
		{time.UTC, []uint32{2}},
		{time.FixedZone("UTC+1", 3600), []uint32{2, 1}},      // 00:30 on the 9th
		{time.FixedZone("UTC-5", -5*3600), []uint32{3, 2}},   // 22:00 on the 9th
		{time.FixedZone("UTC+14", 14*3600), []uint32{1}},     // 13:30 on the 9th; the others are the 10th
		{time.FixedZone("UTC-12", -12*3600), []uint32{3, 2}}, // 15:00 and 00:00 on the 9th; uid 1 is the 8th
	} {
		c.opts.Location = tt.loc // newClient's client is this test's alone
		got, err := c.SearchMessages(context.Background(), "INBOX", ninth)
		if err != nil {
			t.Fatal(err)
		}
		uids := []uint32{}
		for _, m := range got.Messages {
			uids = append(uids, m.UID)
			if m.Date.Location() != tt.loc {
				t.Errorf("%s: uid %d dated %v, not in the owner's zone", tt.loc, m.UID, m.Date)
			}
			if y, mo, d := m.Date.Date(); y != 2026 || mo != time.November || d != 9 {
				t.Errorf("%s: uid %d dated %v, outside the 9th", tt.loc, m.UID, m.Date)
			}
		}
		if !slices.Equal(uids, tt.want) {
			t.Errorf("%s: the 9th = %v, want %v", tt.loc, uids, tt.want)
		}
	}
}

// A message matches by the date it shows. Bridge searches the received
// date, which can be far from the Date header (a synced message can carry
// the sync time); such a message must not show up dated outside the days.
func TestDaysMatchTheShownDate(t *testing.T) {
	t.Parallel()
	c, srv, _ := newClient(t)
	srv.AddMessageAt(t, "INBOX", message("bob@example.org", "Old, synced on the 9th", "Mon, 03 Feb 2020 10:00:00 +0000"),
		time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC))
	got, err := c.SearchMessages(context.Background(), "INBOX", SearchOptions{Since: day("2026-11-09")})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 0 {
		t.Errorf("since the 9th found %+v, dated 2020", got.Messages)
	}
}

// Listing and reading show the date in the owner's zone, not the sender's.
func TestDatesShownInTheOwnersZone(t *testing.T) {
	t.Parallel()
	c, srv, _ := newClient(t)
	london := time.FixedZone("UTC+0", 0)
	c.opts.Location = london
	srv.AddMessage(t, "INBOX", message("bob@example.org", "From California", "Mon, 09 Nov 2026 16:30:00 -0800"))
	ctx := context.Background()
	want := time.Date(2026, 11, 10, 0, 30, 0, 0, london)

	l, err := c.ListMessages(ctx, "INBOX", ListOptions{})
	if err != nil || len(l.Messages) != 1 {
		t.Fatalf("list: %+v, %v", l, err)
	}
	if d := l.Messages[0].Date; !d.Equal(want) || d.Location() != london {
		t.Errorf("listed as %v, want %v", d, want)
	}
	m, err := c.ReadMessage(ctx, "INBOX", l.Messages[0].UID, l.UIDValidity)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Date.Equal(want) || m.Date.Location() != london {
		t.Errorf("read as %v, want %v", m.Date, want)
	}
}

func day(s string) time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return d
}
