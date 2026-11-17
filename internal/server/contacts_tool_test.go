package server

import (
	"strings"
	"testing"
)

func TestFindContactsTool(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.imap.AddMessage(t, "Sent", "From: alice@example.com\r\nTo: Anna Lind <anna@example.org>\r\nDate: Mon, 02 Nov 2026 10:00:00 +0000\r\nSubject: s\r\n\r\nx\r\n")
	e.imap.AddMessage(t, "INBOX", "From: Anna <anna@phish.example.net>\r\nTo: alice@example.com\r\nDate: Tue, 03 Nov 2026 10:00:00 +0000\r\nSubject: s\r\n\r\nx\r\n")

	var out FindContactsOutput
	e.callOK(t, "find_contacts", map[string]any{"query": "anna"}, &out)
	if len(out.People) != 2 || out.People[0].Address != "anna@example.org" || out.People[0].Sent != 1 || out.People[1].Sent != 0 ||
		!strings.Contains(out.Note, "confirm") {
		t.Errorf("find_contacts = %+v", out)
	}
	if res := e.call(t, "find_contacts", map[string]any{"query": ""}); !res.IsError {
		t.Error("an empty query was accepted")
	}
}
