package mail

import (
	"strings"
	"testing"
	"time"
)

// The draft is 7-bit MIME: non-ASCII subject and body are encoded, lines
// end in CRLF, and From is the account address.
func TestBuildDraft(t *testing.T) {
	t.Parallel()
	c := New(Options{User: "alice@example.com"})
	raw, err := c.buildDraft(Draft{
		To: []string{"Åsa <asa@example.com>"}, Bcc: []string{"carol@example.net"},
		Subject: "Mötet", Body: "Hej Åsa,\nses på fredag.\n",
	}, time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{
		"From: <alice@example.com>\r\n", "Bcc: <carol@example.net>\r\n", "Subject: =?utf-8?",
		"Content-Type: text/plain; charset=utf-8\r\n", "Content-Transfer-Encoding: quoted-printable\r\n",
		"Message-Id: <", "Hej =C3=85sa,\r\nses p=C3=A5 fredag.\r\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("draft lacks %q:\n%s", want, s)
		}
	}
	for i := range len(s) {
		if s[i] >= 0x80 {
			t.Fatalf("draft has a non-ASCII byte at %d:\n%s", i, s)
		}
	}
}
