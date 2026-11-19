package server

import (
	"maps"
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
	if len(l.Messages) != 2 || len(l.Messages[0].Labels) != 0 || !strings.Contains(l.Note, "labels as short tags") || l.LabelColors != nil || strings.Contains(l.Note, "label_colors") {
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
	// Only the labels in the listing, by the name they carry (issue #209).
	if !maps.Equal(after.LabelColors, map[string]string{"Travel": "🔵"}) || !strings.Contains(after.Note, "🔵Work") {
		t.Errorf("label colors %v, note %q", after.LabelColors, after.Note)
	}

	var found SearchMessagesOutput
	e.callOK(t, "search_messages", map[string]any{"folder": "INBOX", "subject": l.Messages[0].Subject}, &found)
	if len(found.Messages) != 1 || !slices.Equal(found.Messages[0].Labels, []string{"Travel"}) || found.LabelColors["Travel"] != "🔵" {
		t.Errorf("search_messages = %+v", found.Messages)
	}
}

// list_folders shows the color the owner gave a label; other folders, and
// labels without a color, have none (issue #209).
func TestListFoldersShowsLabelColors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	var out ListFoldersOutput
	e.callOK(t, "list_folders", map[string]any{}, &out)
	seen := false
	for _, f := range out.Folders {
		want := ""
		if f.Name == "Labels/Travel" {
			want, seen = "🔵", true
		}
		if f.Color != want {
			t.Errorf("%s: color %q, want %q", f.Name, f.Color, want)
		}
	}
	if !seen {
		t.Errorf("no Labels/Travel in %+v", out.Folders)
	}
}
