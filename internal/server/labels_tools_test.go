package server

import (
	"slices"
	"strings"
	"testing"
)

// list_messages and search_messages show each message's labels, and tell
// Claude to show them as tags (issue #172). Adding a label through
// add_label shows in the next listing.
func TestListingsShowLabels(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	l := inboxListing(t, e)
	if len(l.Messages) != 2 || len(l.Messages[0].Labels) != 0 || !strings.Contains(l.Note, "labels as short tags") {
		t.Fatalf("before labelling: %+v", l)
	}
	uid := l.Messages[0].UID
	e.callOK(t, "add_label", map[string]any{"folder": "INBOX", "uids": []uint32{uid}, "uidvalidity": l.UIDValidity, "label": "Labels/Travel"}, &map[string]any{})

	var after ListMessagesOutput
	e.callOK(t, "list_messages", map[string]any{"folder": "INBOX"}, &after)
	for _, m := range after.Messages {
		want := []string(nil)
		if m.UID == uid {
			want = []string{"Travel"}
		}
		if !slices.Equal(m.Labels, want) {
			t.Errorf("uid %d: labels %v, want %v", m.UID, m.Labels, want)
		}
	}

	var found SearchMessagesOutput
	e.callOK(t, "search_messages", map[string]any{"folder": "INBOX", "subject": l.Messages[0].Subject}, &found)
	if len(found.Messages) != 1 || !slices.Equal(found.Messages[0].Labels, []string{"Travel"}) {
		t.Errorf("search_messages = %+v", found.Messages)
	}
}
