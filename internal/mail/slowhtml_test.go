package mail

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// A crafted HTML email must not make parsing slow (issue #72): nested,
// unclosed links made the link check quadratic.
func TestPathologicalHTMLIsFast(t *testing.T) {
	t.Parallel()
	const n = 40_000
	var b strings.Builder
	b.WriteString("From: x@example.org\r\nSubject: s\r\nContent-Type: text/html\r\n\r\n<html><body>")
	for i := range n {
		fmt.Fprintf(&b, `<a href="https://example.com/%d">link `, i)
	}
	for range n {
		b.WriteString("</a>")
	}
	b.WriteString("</body></html>")
	t.Logf("message: %d bytes", b.Len())

	start := time.Now()
	m, err := ParseMessage(strings.NewReader(b.String()))
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("parsed in %v, body %d bytes, truncated %v", took, len(m.Body), m.Truncated)
	if took > time.Second {
		t.Errorf("parsing took %v, want well under a second", took)
	}
}

// Many large parts in one message, plain text and HTML, still parse fast and
// return the plain text (issue #72).
func TestManyLargePartsAreFast(t *testing.T) {
	t.Parallel()
	var html strings.Builder
	for i := range 40_000 {
		fmt.Fprintf(&html, `<a href="https://example.com/%d">link `, i)
	}
	var b strings.Builder
	b.WriteString("From: x@example.org\r\nSubject: s\r\nContent-Type: multipart/alternative; boundary=B\r\n\r\n")
	b.WriteString("--B\r\nContent-Type: text/plain\r\n\r\nthe plain text\r\n")
	for range 12 {
		b.WriteString("--B\r\nContent-Type: text/html\r\n\r\n" + html.String() + "\r\n")
	}
	b.WriteString("--B--\r\n")
	t.Logf("message: %d bytes", b.Len())

	start := time.Now()
	m, err := ParseMessage(strings.NewReader(b.String()))
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if m.BodyFormat != "text" || !strings.Contains(m.Body, "the plain text") {
		t.Errorf("body = %q (%s), want the plain text", m.Body[:min(len(m.Body), 80)], m.BodyFormat)
	}
	if took > 2*time.Second {
		t.Errorf("parsing took %v", took)
	}
	t.Logf("parsed in %v", took)
}
