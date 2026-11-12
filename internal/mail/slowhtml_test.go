package mail

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// pathological is an HTML message with n nested, unclosed links, each with
// its own target, then n </a>: the shape that made the link check
// quadratic (issue #72).
func pathological(n int) string {
	var b strings.Builder
	b.WriteString("From: x@example.org\r\nSubject: s\r\nContent-Type: text/html\r\n\r\n<html><body>")
	for i := range n {
		fmt.Fprintf(&b, `<a href="https://example.com/%d">link `, i)
	}
	for range n {
		b.WriteString("</a>")
	}
	b.WriteString("</body></html>")
	return b.String()
}

// bestParse is the fastest of three parses of raw, to damp noise.
func bestParse(t *testing.T, raw string) time.Duration {
	t.Helper()
	best := time.Duration(1<<63 - 1)
	for range 3 {
		start := time.Now()
		if _, err := ParseMessage(strings.NewReader(raw)); err != nil {
			t.Fatal(err)
		}
		best = min(best, time.Since(start))
	}
	return best
}

// A crafted HTML email must not make parsing slow (issue #72). Time grows
// about 4x per doubling when quadratic and about 2x when linear; the ratio
// is checked rather than a fixed time, which depends on the machine and on
// the race detector.
func TestPathologicalHTMLIsFast(t *testing.T) {
	t.Parallel()
	small, large := bestParse(t, pathological(20_000)), bestParse(t, pathological(40_000))
	t.Logf("20,000 links: %v, 40,000 links: %v", small, large)
	if large > 3*small+50*time.Millisecond {
		t.Errorf("doubling the input took %.1fx as long: quadratic", float64(large)/float64(small))
	}
	if large > 10*time.Second {
		t.Errorf("parsing 40,000 links took %v", large)
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
	if took > 10*time.Second {
		t.Errorf("parsing took %v", took)
	}
	t.Logf("parsed in %v", took)
}

// bestConvert is the fastest of three conversions of html with no output
// cap, so only the link check bounds the work.
func bestConvert(html []byte) time.Duration {
	best := time.Duration(1<<63 - 1)
	for range 3 {
		start := time.Now()
		_, _ = htmlToText(html, 1<<30)
		best = min(best, time.Since(start))
	}
	return best
}

// The link check itself is linear, without the output cap that also
// bounds parsing (issue #72).
func TestLinkCheckIsLinear(t *testing.T) {
	t.Parallel()
	body := func(n int) []byte {
		_, html, _ := strings.Cut(pathological(n), "\r\n\r\n") // the body, after the header
		return []byte(html)
	}
	small, large := bestConvert(body(10_000)), bestConvert(body(20_000))
	t.Logf("10,000 links: %v, 20,000 links: %v", small, large)
	if large > 3*small+50*time.Millisecond {
		t.Errorf("doubling the input took %.1fx as long: quadratic", float64(large)/float64(small))
	}
}
