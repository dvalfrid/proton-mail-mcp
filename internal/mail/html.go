package mail

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	maxHTMLTokenBytes = 256 << 10 // one token (a tag with its attributes, or a text run)
	maxLinkRunes      = 300
)

// skipped elements: their content is code, metadata or embedded objects, not
// text the reader sees. Nothing inside is fetched or run.
var skipped = map[atom.Atom]bool{
	atom.Head: true, atom.Title: true, atom.Script: true, atom.Style: true,
	atom.Noscript: true, atom.Template: true, atom.Svg: true, atom.Math: true,
	atom.Iframe: true, atom.Object: true, atom.Embed: true, atom.Applet: true,
	atom.Frameset: true, atom.Select: true, atom.Button: true,
}

// blocks start and end on their own line.
var blocks = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Section: true, atom.Article: true,
	atom.Header: true, atom.Footer: true, atom.Main: true, atom.Aside: true,
	atom.Nav: true, atom.Blockquote: true, atom.Pre: true, atom.Table: true,
	atom.Tr: true, atom.Ul: true, atom.Ol: true, atom.Dl: true, atom.Dt: true,
	atom.Dd: true, atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true,
	atom.H5: true, atom.H6: true, atom.Hr: true, atom.Form: true,
	atom.Fieldset: true, atom.Figure: true, atom.Address: true, atom.Center: true,
}

// RemoteImage is an image a message shows from the internet: listed, never
// fetched by reading. Its address and alt text are written by the sender.
type RemoteImage struct {
	Index int    `json:"index" jsonschema:"the image's number, as [image N] marks it in the body"`
	URL   string `json:"url"`
	Alt   string `json:"alt,omitempty"`
}

const (
	maxRemoteImages = 100  // images numbered per message; the rest are marked without a number
	maxImageURL     = 2048 // a longer address is marked, not listed
	maxAltRunes     = 200
)

// htmlToText is htmlToTextImages without the list of remote images.
func htmlToText(data []byte, maxOut int) (string, bool) {
	text, _, cut := htmlToTextImages(data, maxOut, 1)
	return text, cut
}

// htmlToTextImages converts sender-written HTML to plain text: the visible
// text, with line breaks for blocks and link targets in angle brackets.
// Scripts, styles, embedded objects and text hidden with the hidden
// attribute or display:none / visibility:hidden are dropped. It reports
// whether the input was cut short.
//
// Every image a reader would see is marked where it stands: an image from
// the internet as [image N] (or [image N: alt]), numbered from first and
// returned in the list; an image embedded in the message as [embedded
// image]; any other as [image]. Hidden images and 1×1 tracking pixels are
// left out. Nothing is fetched (issue #149).
//
// It stops once the text is longer than maxOut bytes (reported as cut), so
// a large part costs no more than the text that will be returned.
func htmlToTextImages(data []byte, maxOut, first int) (string, []RemoteImage, bool) {
	var images []RemoteImage
	text, cut := convertHTML(data, maxOut, func(attrs map[string]string) string {
		return imageMarker(attrs, first, &images)
	})
	return text, images, cut
}

// imageMarker is the text for one <img>, adding it to images if it is from
// the internet.
func imageMarker(attrs map[string]string, first int, images *[]RemoteImage) string {
	if hidden(attrs) || tiny(attrs["width"]) || tiny(attrs["height"]) {
		return ""
	}
	alt := clean(strings.TrimSpace(attrs["alt"]), maxAltRunes)
	with := func(s string) string {
		if alt != "" {
			return "[" + s + ": " + alt + "]"
		}
		return "[" + s + "]"
	}
	src := strings.TrimSpace(attrs["src"])
	lower := strings.ToLower(src)
	switch {
	case strings.HasPrefix(lower, "cid:"):
		return with("embedded image")
	case (strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://")) &&
		len(src) <= maxImageURL && !strings.ContainsFunc(src, unsafeURLRune) && len(*images) < maxRemoteImages:
		n := first + len(*images)
		*images = append(*images, RemoteImage{Index: n, URL: src, Alt: alt})
		return with(fmt.Sprintf("image %d", n))
	}
	return with("image")
}

// tiny is a width or height of at most 1 pixel: a tracking pixel, not an
// image a reader sees.
func tiny(v string) bool {
	v = strings.TrimSuffix(strings.TrimSpace(v), "px")
	return v == "0" || v == "1"
}

// unsafeURLRune is a rune that has no place in an image address shown back
// to Claude: spaces, controls and format characters (bidi overrides).
func unsafeURLRune(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
}

// convertHTML does the conversion; img gives the text for each <img>.
func convertHTML(data []byte, maxOut int, img func(map[string]string) string) (string, bool) {
	z := html.NewTokenizer(bytes.NewReader(data))
	z.SetMaxBuf(maxHTMLTokenBytes)

	var (
		b         strings.Builder
		skipName  string // element whose content is being dropped
		skipDepth int    // nesting of that element inside itself
		pre       int    // inside <pre>: keep whitespace
		links     []link // each open <a>
	)
	newline := func() {
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteByte('\n')
		}
	}

	for {
		if b.Len() > maxOut {
			return b.String(), true
		}
		tt := z.Next()
		if tt == html.ErrorToken {
			return b.String(), !errors.Is(z.Err(), io.EOF)
		}
		name, hasAttr := z.TagName()
		tag := atom.Lookup(name)

		if skipDepth > 0 {
			if string(name) == skipName {
				switch tt { //nolint:exhaustive // only nesting of the skipped tag matters
				case html.StartTagToken:
					skipDepth++
				case html.EndTagToken:
					skipDepth--
				}
			}
			continue
		}

		switch tt { //nolint:exhaustive // comments and doctypes carry no visible text
		case html.TextToken:
			text := string(z.Text())
			if pre == 0 {
				text = collapseSpace(text, strings.HasSuffix(b.String(), "\n") || b.Len() == 0)
			}
			b.WriteString(text)

		case html.StartTagToken, html.SelfClosingTagToken:
			attrs := readAttrs(z, hasAttr)
			if tt == html.StartTagToken && !isVoid(tag) && (skipped[tag] || hidden(attrs)) {
				skipName, skipDepth = string(name), 1
				continue
			}
			switch {
			case tag == atom.Br:
				b.WriteByte('\n')
			case tag == atom.Li:
				newline()
				b.WriteString("- ")
			case tag == atom.Td || tag == atom.Th:
				if !strings.HasSuffix(b.String(), "\n") && b.Len() > 0 {
					b.WriteString("\t")
				}
			case tag == atom.Img:
				b.WriteString(img(attrs))
			case tag == atom.A && tt == html.StartTagToken:
				links = append(links, link{href: linkTarget(attrs["href"]), start: b.Len()})
			case blocks[tag]:
				newline()
				if tag == atom.Pre {
					pre++
				}
			}

		case html.EndTagToken:
			switch {
			case tag == atom.A && len(links) > 0:
				// Show the target unless the link text already is it.
				if l := links[len(links)-1]; l.href != "" && !linkTextIs(b.String()[l.start:], l.href) {
					b.WriteString(" <" + l.href + ">")
				}
				links = links[:len(links)-1]
			case blocks[tag] || tag == atom.Li:
				newline()
				if tag == atom.Pre && pre > 0 {
					pre--
				}
			}
		}
	}
}

type link struct {
	href  string // "" if not shown
	start int    // length of the output when the link opened
}

func readAttrs(z *html.Tokenizer, more bool) map[string]string {
	attrs := map[string]string{}
	for more {
		var k, v []byte
		k, v, more = z.TagAttr()
		attrs[string(k)] = string(v)
	}
	return attrs
}

// hidden reports whether an element is hidden from the reader, so text a
// sender hides (a common prompt-injection trick) is not shown either.
func hidden(attrs map[string]string) bool {
	if _, ok := attrs["hidden"]; ok {
		return true
	}
	if strings.EqualFold(attrs["aria-hidden"], "true") {
		return true
	}
	style := strings.ToLower(strings.Join(strings.Fields(attrs["style"]), ""))
	for _, s := range []string{"display:none", "visibility:hidden", "font-size:0;", "font-size:0px", "max-height:0;", "max-height:0px", "opacity:0;"} {
		if strings.Contains(style+";", s) {
			return true
		}
	}
	return false
}

// linkTarget returns a link worth showing, or "". Only web and mail links
// are shown; javascript:, data: and the like are dropped.
func linkTarget(href string) string {
	href = strings.TrimSpace(href)
	lower := strings.ToLower(href)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") && !strings.HasPrefix(lower, "mailto:") {
		return ""
	}
	return clean(href, maxLinkRunes)
}

func isVoid(a atom.Atom) bool {
	switch a { //nolint:exhaustive // only void elements matter
	case atom.Area, atom.Base, atom.Br, atom.Col, atom.Embed, atom.Hr, atom.Img,
		atom.Input, atom.Link, atom.Meta, atom.Source, atom.Track, atom.Wbr:
		return true
	}
	return false
}

// collapseSpace turns runs of whitespace into one space, as a browser does
// outside <pre>. Leading space is dropped at the start of a line.
func collapseSpace(s string, atLineStart bool) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		if s != "" && !atLineStart {
			return " "
		}
		return ""
	}
	out := strings.Join(fields, " ")
	if !atLineStart && s[0] <= ' ' {
		out = " " + out
	}
	if s[len(s)-1] <= ' ' {
		out += " "
	}
	return out
}

// linkTextIs reports whether a link's text already shows its target, so
// the target isn't repeated. It looks only at the start of the text, a
// little longer than the target: searching all text since the link opened
// made nested, unclosed links quadratic (issue #72).
func linkTextIs(text, href string) bool {
	if n := len(href) + 64; len(text) > n {
		text = text[:n]
	}
	return strings.Contains(text, href)
}
