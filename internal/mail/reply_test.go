package mail

import (
	"slices"
	"strings"
	"testing"
	"time"
)

const original = "From: Bob <bob@example.org>\r\n" +
	"To: alice@example.com, Dave <dave@example.net>\r\n" +
	"Cc: carol@example.org\r\n" +
	"Subject: Lunch?\r\n" +
	"Date: Mon, 02 Nov 2026 10:00:00 +0000\r\n" +
	"Message-ID: <m1@example.org>\r\n" +
	"References: <m0@example.org>\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n\r\n" +
	"Ska vi äta lunch?\r\nKl 12?\r\n"

func header(raw, name, value string) string {
	return name + ": " + value + "\r\n" + raw
}

// A reply goes to the sender (Reply-To first), with everyone else only on
// reply-all, never to the owner; "Re:" isn't doubled; the threading headers
// and the quoted original are there.
func TestReply(t *testing.T) {
	t.Parallel()
	c := New(Options{User: "alice@example.com", Location: time.UTC})
	for name, tc := range map[string]struct {
		raw     string
		all     bool
		to, cc  []string
		subject string
	}{
		"sender":    {original, false, []string{`"Bob" <bob@example.org>`}, nil, "Re: Lunch?"},
		"reply all": {original, true, []string{`"Bob" <bob@example.org>`}, []string{`"Dave" <dave@example.net>`, "<carol@example.org>"}, "Re: Lunch?"},
		"reply-to":  {header(original, "Reply-To", "list@example.org"), false, []string{"<list@example.org>"}, nil, "Re: Lunch?"},
		"re kept":   {strings.Replace(original, "Subject: Lunch?", "Subject: RE: Lunch?", 1), false, []string{`"Bob" <bob@example.org>`}, nil, "RE: Lunch?"},
		"own mail": {strings.Replace(original, "From: Bob <bob@example.org>", "From: alice@example.com", 1), false,
			[]string{`"Dave" <dave@example.net>`}, nil, "Re: Lunch?"},
	} {
		d, err := c.reply([]byte(tc.raw), Reply{Body: "Gärna!\n", All: tc.all})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !slices.Equal(d.To, tc.to) || !slices.Equal(d.Cc, tc.cc) || d.Subject != tc.subject {
			t.Errorf("%s: to %q cc %q subject %q", name, d.To, d.Cc, d.Subject)
		}
		if d.InReplyTo != "m1@example.org" || !slices.Equal(d.References, []string{"m0@example.org", "m1@example.org"}) {
			t.Errorf("%s: in-reply-to %q references %q", name, d.InReplyTo, d.References)
		}
		if !strings.HasPrefix(d.Body, "Gärna!\n\nOn Mon, 2 Nov 2026 10:00, ") || !strings.Contains(d.Body, "> Ska vi äta lunch?\n> Kl 12?\n") {
			t.Errorf("%s: body %q", name, d.Body)
		}
		raw, err := c.buildDraft(d, time.Now())
		if err != nil {
			t.Fatalf("%s: build: %v", name, err)
		}
		for _, want := range []string{"In-Reply-To: <m1@example.org>\r\n", "References: <m0@example.org> <m1@example.org>\r\n"} {
			if !strings.Contains(string(raw), want) {
				t.Errorf("%s: draft lacks %q", name, want)
			}
		}
	}
}

// A message with nobody to reply to is refused; a long original is cut so
// the draft stays within the size limit.
func TestReplyLimits(t *testing.T) {
	t.Parallel()
	c := New(Options{User: "alice@example.com", Location: time.UTC})
	if _, err := c.reply([]byte("Subject: x\r\n\r\nhi\r\n"), Reply{Body: "b"}); err == nil || !strings.Contains(err.Error(), "no address to reply to") {
		t.Errorf("no sender: %v", err)
	}
	long := strings.Replace(original, "Ska vi äta lunch?", strings.Repeat("åäö ", MaxDraftBodyBytes/4), 1)
	d, err := c.reply([]byte(long), Reply{Body: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Body) > MaxDraftBodyBytes || !strings.HasPrefix(d.Body, "ok") {
		t.Errorf("body is %d bytes, want at most %d", len(d.Body), MaxDraftBodyBytes)
	}
	if _, err := c.buildDraft(d, time.Now()); err != nil {
		t.Errorf("build: %v", err)
	}
}
