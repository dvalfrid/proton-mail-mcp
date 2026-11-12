package help

import (
	"os"
	"strings"
	"testing"
	"unicode"
)

// anchor is the id GitHub gives a heading: lower case, punctuation dropped,
// spaces to hyphens.
func anchor(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	return b.String()
}

// Every topic an error links to has its section, so no Help link is broken.
func TestEveryTopicHasASection(t *testing.T) {
	data, err := os.ReadFile("../../docs/troubleshooting.md")
	if err != nil {
		t.Fatal(err)
	}
	anchors := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		if h, ok := strings.CutPrefix(strings.TrimSpace(line), "## "); ok {
			anchors[anchor(h)] = true
		}
	}
	for _, topic := range Topics {
		if !anchors[string(topic)] {
			t.Errorf("docs/troubleshooting.md has no section for %q (headings: %v)", topic, anchors)
		}
	}
}
