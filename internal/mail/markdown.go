package mail

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

// The Markdown a draft may use (issue #188), turned into HTML here, so the
// HTML in a draft is always the server's own: Claude's text is escaped
// first, and only these marks become tags. No raw HTML, no images, no
// scripts or styles, and links only to https and mailto.
//
//	# Heading, ## Heading, ### Heading
//	**bold**, *italic*, _italic_, `code`
//	[text](https://example.org), [text](mailto:a@example.org)
//	- item, * item, 1. item
//	> quote
//
// A blank line ends a paragraph; a single line break stays a line break.

var (
	mdCode    = regexp.MustCompile("`([^`\n]+)`")
	mdLink    = regexp.MustCompile(`\[([^\]\n]+)\]\(([^)\s]+)\)`)
	mdBold    = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	mdItalic  = regexp.MustCompile(`(^|[^\w*])\*([^*\s][^*\n]*)\*`)
	mdItalic2 = regexp.MustCompile(`(^|[^\w])_([^_\s][^_\n]*)_`)
	mdHeading = regexp.MustCompile(`^(#{1,3}) +(.+)$`)
	mdBullet  = regexp.MustCompile(`^[-*] +(.*)$`)
	mdNumber  = regexp.MustCompile(`^\d{1,9}[.)] +(.*)$`)
)

// markdownHTML turns s, Markdown written for a draft, into HTML.
func markdownHTML(s string) string {
	var b strings.Builder
	var para []string
	list := "" // "ul" or "ol" while a list is open
	flush := func() {
		if len(para) > 0 {
			b.WriteString("<p>" + strings.Join(para, "<br>\n") + "</p>\n")
			para = nil
		}
	}
	closeList := func() {
		if list != "" {
			b.WriteString("</" + list + ">\n")
			list = ""
		}
	}
	openList := func(kind string) {
		if list != kind {
			closeList()
			b.WriteString("<" + kind + ">\n")
			list = kind
		}
	}
	var quote []string
	flushQuote := func() {
		if len(quote) > 0 {
			b.WriteString("<blockquote>" + strings.Join(quote, "<br>\n") + "</blockquote>\n")
			quote = nil
		}
	}

	for line := range strings.SplitSeq(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		t := strings.TrimRight(line, " \t")
		if q, ok := strings.CutPrefix(t, ">"); ok {
			flush()
			closeList()
			quote = append(quote, inline(strings.TrimPrefix(q, " ")))
			continue
		}
		flushQuote()
		switch {
		case strings.TrimSpace(t) == "":
			flush()
			closeList()
		case mdHeading.MatchString(t):
			flush()
			closeList()
			m := mdHeading.FindStringSubmatch(t)
			level := len(m[1]) + 1 // # is h2: the subject is the mail's title
			fmt.Fprintf(&b, "<h%d>%s</h%d>\n", level, inline(m[2]), level)
		case mdBullet.MatchString(t):
			flush()
			openList("ul")
			b.WriteString("<li>" + inline(mdBullet.FindStringSubmatch(t)[1]) + "</li>\n")
		case mdNumber.MatchString(t):
			flush()
			openList("ol")
			b.WriteString("<li>" + inline(mdNumber.FindStringSubmatch(t)[1]) + "</li>\n")
		default:
			closeList()
			para = append(para, inline(t))
		}
	}
	flushQuote()
	flush()
	closeList()
	return b.String()
}

// inline escapes s and turns its inline marks into tags. Code spans are
// kept apart first, so nothing inside them is formatted.
func inline(s string) string {
	var codes []string
	s = mdCode.ReplaceAllStringFunc(s, func(m string) string {
		codes = append(codes, "<code>"+html.EscapeString(m[1:len(m)-1])+"</code>")
		return fmt.Sprintf("\x00%d\x00", len(codes)-1)
	})
	s = html.EscapeString(s)
	s = mdLink.ReplaceAllStringFunc(s, func(m string) string {
		p := mdLink.FindStringSubmatch(m)
		text, url := p[1], p[2]
		if !safeLink(html.UnescapeString(url)) {
			return text + " (" + url + ")"
		}
		return `<a href="` + url + `">` + text + `</a>`
	})
	s = mdBold.ReplaceAllString(s, "<strong>$1</strong>")
	s = mdItalic.ReplaceAllString(s, "$1<em>$2</em>")
	s = mdItalic2.ReplaceAllString(s, "$1<em>$2</em>")
	for i, c := range codes {
		s = strings.Replace(s, fmt.Sprintf("\x00%d\x00", i), c, 1)
	}
	return s
}

// safeLink allows https and mailto links, nothing that runs or reaches this
// computer.
func safeLink(url string) bool {
	l := strings.ToLower(url)
	return (strings.HasPrefix(l, "https://") && len(l) > len("https://")) ||
		(strings.HasPrefix(l, "mailto:") && strings.Contains(l, "@"))
}

// quotedHTML is the original below a reply or forward, as HTML: written by
// its sender, so it is only escaped, never formatted, with its "> " lines
// in a blockquote.
func quotedHTML(s string) string {
	var b strings.Builder
	var quote, plain []string
	flushQuote := func() {
		if len(quote) > 0 {
			b.WriteString("<blockquote>" + strings.Join(quote, "<br>\n") + "</blockquote>\n")
			quote = nil
		}
	}
	flushPlain := func() {
		if len(plain) > 0 {
			b.WriteString("<p>" + strings.Join(plain, "<br>\n") + "</p>\n")
			plain = nil
		}
	}
	for line := range strings.SplitSeq(strings.Trim(strings.ReplaceAll(s, "\r\n", "\n"), "\n"), "\n") {
		if q, ok := strings.CutPrefix(line, ">"); ok {
			flushPlain()
			quote = append(quote, html.EscapeString(strings.TrimPrefix(q, " ")))
			continue
		}
		flushQuote()
		plain = append(plain, html.EscapeString(line))
	}
	flushQuote()
	flushPlain()
	return b.String()
}

// draftHTML is the HTML part of a Markdown draft: the owner's text as
// Markdown, then the original below it, escaped.
func draftHTML(body, quoted string) string {
	return "<!DOCTYPE html>\n<html><head><meta charset=\"utf-8\"></head><body>\n" +
		markdownHTML(body) + quotedHTML(quoted) + "</body></html>\n"
}
