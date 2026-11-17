package mail

import (
	"bytes"
	"strings"
	"testing"
	"time"

	gomessage "github.com/emersion/go-message"
)

func TestMarkdownHTML(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"Hello **you** and *them* and _us_.":             "<p>Hello <strong>you</strong> and <em>them</em> and <em>us</em>.</p>\n",
		"Line one\nline two\n\nNext":                     "<p>Line one<br>\nline two</p>\n<p>Next</p>\n",
		"# Plan\n- a\n- b\n\n1. one\n2. two":             "<h2>Plan</h2>\n<ul>\n<li>a</li>\n<li>b</li>\n</ul>\n<ol>\n<li>one</li>\n<li>two</li>\n</ol>\n",
		"> quoted **text**":                              "<blockquote>quoted <strong>text</strong></blockquote>\n",
		"See [the site](https://example.org/a?b=1&c=2).": `<p>See <a href="https://example.org/a?b=1&amp;c=2">the site</a>.</p>` + "\n",
		"Mail [me](mailto:a@example.org)":                `<p>Mail <a href="mailto:a@example.org">me</a></p>` + "\n",
		"Use `**not bold**` here":                        "<p>Use <code>**not bold**</code> here</p>\n",
		"2 * 3 * 4 = 24, snake_case_name":                "<p>2 * 3 * 4 = 24, snake_case_name</p>\n",
	} {
		if got := markdownHTML(in); got != want {
			t.Errorf("markdownHTML(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

// Nothing Claude writes becomes HTML of its own: tags are escaped, and only
// https and mailto links are made.
func TestMarkdownHTMLIsSafe(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		`<script>alert(1)</script>`,
		`<img src="https://tracker.example.net/p.gif">`,
		`[click](javascript:alert(1))`,
		`[local](http://127.0.0.1:1143/)`,
		`[file](file:///C:/secret.txt)`,
		`[x](https://example.org" onclick="alert(1))`,
		`<a href="https://example.org">raw</a>`,
	} {
		out := markdownHTML(in)
		if strings.Contains(out, "<script") || strings.Contains(out, "<img") || strings.Contains(out, `href="javascript`) ||
			strings.Contains(out, `href="http://`) || strings.Contains(out, `href="file`) || strings.Contains(out, `" onclick`) {
			t.Errorf("markdownHTML(%q) = %q", in, out)
		}
	}
}

// The original below a reply is escaped and never formatted: a sender's
// Markdown or HTML stays text.
func TestQuotedHTML(t *testing.T) {
	t.Parallel()
	got := quotedHTML("\n\nOn Mon, Bob wrote:\n> **buy** [now](https://phish.example.net)\n> <b>x</b>\n")
	want := "<p>On Mon, Bob wrote:</p>\n<blockquote>**buy** [now](https://phish.example.net)<br>\n&lt;b&gt;x&lt;/b&gt;</blockquote>\n"
	if got != want {
		t.Errorf("quotedHTML\n got %q\nwant %q", got, want)
	}
}

// A Markdown draft is multipart/alternative of the text and the HTML, with
// attachments beside it; a plain one is unchanged.
func TestBuildFormattedDraft(t *testing.T) {
	t.Parallel()
	c, _, _ := newClient(t)
	d := Draft{To: []string{"bob@example.org"}, Subject: "Plan", Body: "**Hi** Bob", Quoted: "\n\n> old", Markdown: true,
		Attachments: []DraftFile{{Name: "a.txt", ContentType: "text/plain", Data: []byte("file")}}}
	raw, err := c.buildDraft(d, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	e, err := gomessage.Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	var htmlPart string
	err = e.Walk(func(_ []int, p *gomessage.Entity, err error) error {
		if err != nil {
			return err
		}
		mt, _, _ := p.Header.ContentType()
		types = append(types, mt)
		if mt == "text/html" {
			var b bytes.Buffer
			_, _ = b.ReadFrom(p.Body)
			htmlPart = b.String()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(types, ",") != "multipart/mixed,multipart/alternative,text/plain,text/html,text/plain" {
		t.Errorf("parts %v", types)
	}
	if !strings.Contains(htmlPart, "<strong>Hi</strong> Bob") || !strings.Contains(htmlPart, "<blockquote>old</blockquote>") {
		t.Errorf("html part %q", htmlPart)
	}
	m, err := ParseMessage(bytes.NewReader(raw))
	if err != nil || !strings.HasPrefix(m.Body, "**Hi** Bob") {
		t.Errorf("text as read back %q, %v", m.Body, err)
	}
}
