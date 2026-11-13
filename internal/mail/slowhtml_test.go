package mail

import (
	"fmt"
	"runtime"
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

// bestOf times small and large in turn, five times each after a garbage
// collection, and returns the fastest of each. Taking turns means a busy
// moment on the machine slows both, not only one, which a ratio check needs
// (issue #156).
func bestOf(small, large func()) (time.Duration, time.Duration) {
	bestS, bestL := time.Duration(1<<63-1), time.Duration(1<<63-1)
	timed := func(f func()) time.Duration {
		runtime.GC()
		start := time.Now()
		f()
		return time.Since(start)
	}
	for range 5 {
		bestS = min(bestS, timed(small))
		bestL = min(bestL, timed(large))
	}
	return bestS, bestL
}

// parse parses raw, failing the test on an error.
func parse(t *testing.T, raw string) func() {
	t.Helper()
	return func() {
		if _, err := ParseMessage(strings.NewReader(raw)); err != nil {
			t.Error(err)
		}
	}
}

// A crafted HTML email must not make parsing slow (issue #72). Time grows
// about 4x per doubling when quadratic and about 2x when linear; the ratio
// is checked rather than a fixed time, which depends on the machine and on
// the race detector. The timing tests don't run in parallel: Go runs them
// before the parallel tests, so other tests don't skew one measurement
// (issue #156).
func TestPathologicalHTMLIsFast(t *testing.T) { //nolint:paralleltest // timed: must not share the CPU with other tests
	small, large := bestOf(parse(t, pathological(20_000)), parse(t, pathological(40_000)))
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
func TestManyLargePartsAreFast(t *testing.T) { //nolint:paralleltest // timed, and heavy: kept off the timing tests above
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

// convert converts html with no output cap, so only the link check bounds
// the work.
func convert(html []byte) func() {
	return func() { _, _ = htmlToText(html, 1<<30) }
}

// The link check itself is linear, without the output cap that also
// bounds parsing (issue #72).
func TestLinkCheckIsLinear(t *testing.T) { //nolint:paralleltest // timed: must not share the CPU with other tests
	body := func(n int) []byte {
		_, html, _ := strings.Cut(pathological(n), "\r\n\r\n") // the body, after the header
		return []byte(html)
	}
	small, large := bestOf(convert(body(10_000)), convert(body(20_000)))
	t.Logf("10,000 links: %v, 20,000 links: %v", small, large)
	if large > 3*small+50*time.Millisecond {
		t.Errorf("doubling the input took %.1fx as long: quadratic", float64(large)/float64(small))
	}
}
