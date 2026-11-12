package mail

import (
	"errors"
	"io"
	"mime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	gomessage "github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset" // registers non-UTF-8 charsets for decoding
	gomail "github.com/emersion/go-message/mail"
)

// Limits on what parsing a message reads and returns. Everything in a
// message is written by the sender, so every dimension is capped.
const (
	MaxMessageBytes   = 32 << 20 // raw message read at most; the rest is ignored
	MaxBodyRunes      = 30_000   // body text returned
	MaxAttachments    = 50       // attachments listed
	MaxAddresses      = 50       // per address header (To, Cc)
	maxPartBytes      = 2 << 20  // read from one text part
	maxParts          = 200      // MIME parts visited
	maxDepth          = 10       // multipart nesting followed
	maxFilenameRunes  = 200
	maxMessageIDRunes = 300
	maxHeaderBytes    = 256 << 10
)

// TruncatedMarker ends a body that was cut at MaxBodyRunes.
const TruncatedMarker = "\n\n[message truncated]"

// Message is a parsed message. Every field is written by the sender: it is
// untrusted data, never instructions.
type Message struct {
	From        []string     `json:"from"`
	To          []string     `json:"to,omitempty"`
	Cc          []string     `json:"cc,omitempty"`
	Date        time.Time    `json:"date"`
	Subject     string       `json:"subject"`
	MessageID   string       `json:"message_id,omitempty"`
	Body        string       `json:"body"`
	BodyFormat  string       `json:"body_format" jsonschema:"text (from text/plain), html (converted to text) or empty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	Truncated   bool         `json:"truncated" jsonschema:"true if a limit cut the body, parts or lists"`
}

// Attachment is listed only: never opened, saved or executed.
type Attachment struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Size   int64  `json:"size_bytes" jsonschema:"decoded size"`
	Inline bool   `json:"inline" jsonschema:"embedded in the message, e.g. an image in HTML"`
}

// ErrUnreadableMessage means not even the header could be parsed.
var ErrUnreadableMessage = errors.New("the message cannot be parsed: its header is malformed")

// ParseMessage turns a raw RFC 5322 message into capped, cleaned text. It
// never fetches anything: remote resources in HTML are dropped. A damaged
// message gives what could be read, not an error; only an unreadable header
// is an error.
func ParseMessage(r io.Reader) (*Message, error) {
	limited := &io.LimitedReader{R: r, N: MaxMessageBytes}
	e, err := gomessage.ReadWithOptions(limited, &gomessage.ReadOptions{MaxHeaderBytes: maxHeaderBytes})
	if err != nil && !gomessage.IsUnknownCharset(err) && !gomessage.IsUnknownEncoding(err) {
		// The parser's error quotes the offending header line, which the
		// sender wrote: it must not reach the model outside untrusted
		// content, so only the fixed error is returned (issue #73).
		return nil, ErrUnreadableMessage
	}

	p := &parser{msg: &Message{}}
	p.headers(gomail.Header{Header: e.Header})
	p.walk(e, 0)

	m := p.msg
	switch {
	case len(p.plain) > 0:
		m.Body, m.BodyFormat = joinCapped(p.plain, maxBodyBytes, m), "text"
	case len(p.html) > 0:
		// HTML is converted only when there is no plain text to use.
		texts := make([]string, 0, len(p.html))
		for _, data := range p.html {
			text, cut := htmlToText(data, maxBodyBytes)
			if cut {
				m.Truncated = true
			}
			texts = append(texts, text)
		}
		m.Body, m.BodyFormat = joinCapped(texts, maxBodyBytes, m), "html"
	}
	m.Body = cleanBody(m.Body)
	if r := []rune(m.Body); len(r) > MaxBodyRunes {
		m.Body = string(r[:MaxBodyRunes]) + TruncatedMarker
		m.Truncated = true
	}
	if limited.N <= 0 {
		m.Truncated = true
	}
	return m, nil
}

type parser struct {
	msg   *Message
	plain []string
	html  [][]byte // raw HTML parts, converted only if there is no plain text
	parts int
}

// maxBodyBytes bounds the body text kept before it is cut to MaxBodyRunes:
// no rune is longer than four bytes. Parts aren't joined or converted in
// full only to be cut (issue #72).
const maxBodyBytes = MaxBodyRunes * 4

// joinCapped joins parts with blank lines, stopping after limit bytes and
// marking m truncated if it does.
func joinCapped(parts []string, limit int, m *Message) string {
	var b strings.Builder
	for i, part := range parts {
		if i > 0 {
			b.WriteString("\n\n")
		}
		if b.Len()+len(part) > limit {
			cut := part[:max(limit-b.Len(), 0)]
			for len(cut) > 0 && !utf8.ValidString(cut) { // don't split a character
				cut = cut[:len(cut)-1]
			}
			b.WriteString(cut)
			m.Truncated = true
			break
		}
		b.WriteString(part)
	}
	return b.String()
}

func (p *parser) headers(h gomail.Header) {
	m := p.msg
	m.From = p.addresses(h, "From")
	m.To = p.addresses(h, "To")
	m.Cc = p.addresses(h, "Cc")
	if d, err := h.Date(); err == nil {
		m.Date = d
	}
	subject, err := h.Subject()
	if err != nil {
		subject = h.Get("Subject")
	}
	m.Subject = clean(subject, maxSubjectRunes)
	if id, err := h.MessageID(); err == nil {
		m.MessageID = clean(id, maxMessageIDRunes)
	}
}

func (p *parser) addresses(h gomail.Header, key string) []string {
	list, err := h.AddressList(key)
	if err != nil {
		// Unparseable: show the raw header text, cleaned, as one entry.
		if raw := clean(h.Get(key), maxNameRunes*2); raw != "" {
			return []string{raw}
		}
		return nil
	}
	out := make([]string, 0, min(len(list), MaxAddresses))
	for i, a := range list {
		if i == MaxAddresses {
			p.msg.Truncated = true
			break
		}
		out = append(out, formatNameAddr(a.Name, a.Address))
	}
	return out
}

func (p *parser) walk(e *gomessage.Entity, depth int) {
	if p.parts >= maxParts || depth > maxDepth {
		p.msg.Truncated = true
		return
	}
	p.parts++

	if mr := e.MultipartReader(); mr != nil {
		for {
			part, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil && !gomessage.IsUnknownCharset(err) && !gomessage.IsUnknownEncoding(err) {
				// A broken boundary or header: keep what was read so far.
				p.msg.Truncated = true
				return
			}
			p.walk(part, depth+1)
			if p.parts >= maxParts {
				p.msg.Truncated = true
				return
			}
		}
	}

	mediaType, _, _ := e.Header.ContentType()
	if mediaType == "" {
		mediaType = "text/plain" // RFC 2045 default
	}
	disposition, _, _ := e.Header.ContentDisposition()
	isText := mediaType == "text/plain" || mediaType == "text/html"

	if isText && disposition != "attachment" {
		data, cut := readCapped(e.Body, maxPartBytes)
		if cut {
			p.msg.Truncated = true
		}
		if mediaType == "text/html" {
			p.html = append(p.html, data)
		} else {
			p.plain = append(p.plain, toValidUTF8(data))
		}
		return
	}
	p.attachment(e, mediaType, disposition)
}

func (p *parser) attachment(e *gomessage.Entity, mediaType, disposition string) {
	size, _ := io.Copy(io.Discard, e.Body)
	if len(p.msg.Attachments) >= MaxAttachments {
		p.msg.Truncated = true
		return
	}
	name, err := (&gomail.AttachmentHeader{Header: e.Header}).Filename()
	if err != nil || name == "" {
		_, params, _ := e.Header.ContentType()
		name = decodeWord(params["name"])
	}
	p.msg.Attachments = append(p.msg.Attachments, Attachment{
		Name:   clean(name, maxFilenameRunes),
		Type:   clean(mediaType, maxNameRunes),
		Size:   size,
		Inline: disposition == "inline" || (disposition == "" && e.Header.Get("Content-Id") != ""),
	})
}

func decodeWord(s string) string {
	if d, err := new(mime.WordDecoder).DecodeHeader(s); err == nil {
		return d
	}
	return s
}

// readCapped reads at most limit bytes, discards the rest, and reports
// whether anything was cut. A read error keeps what was read before it.
func readCapped(r io.Reader, limit int64) ([]byte, bool) {
	data, _ := io.ReadAll(io.LimitReader(r, limit))
	rest, _ := io.Copy(io.Discard, r)
	return data, rest > 0
}

func toValidUTF8(b []byte) string {
	return strings.ToValidUTF8(string(b), "�")
}

// cleanBody normalises line endings and removes characters that can hide or
// fake text: control characters other than newline and tab, and Unicode
// format characters (bidi overrides, zero-width characters). Runs of blank
// lines are collapsed.
func cleanBody(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	var b strings.Builder
	b.Grow(len(s))
	newlines := 0
	for _, r := range s {
		switch {
		case r == '\n':
			newlines++
			if newlines <= 2 {
				b.WriteRune(r)
			}
			continue
		case r == '\t':
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			continue
		}
		newlines = 0
		b.WriteRune(r)
	}
	lines := strings.Split(b.String(), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRightFunc(l, unicode.IsSpace)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
