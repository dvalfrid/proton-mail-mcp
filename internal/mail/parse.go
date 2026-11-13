package mail

import (
	"errors"
	"fmt"
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
	// RemoteImages are the images an HTML body shows from the internet,
	// marked [image N] in Body. They are never fetched by reading.
	RemoteImages []RemoteImage `json:"remote_images,omitempty" jsonschema:"images the message shows from the internet, marked [image N] in the body; not fetched"`
	Truncated    bool          `json:"truncated" jsonschema:"true if a limit cut the body, parts or lists"`
}

// Attachment is listed by ParseMessage: never opened, saved or executed.
// Only an image is ever decoded, and only when asked for by Index (Image).
type Attachment struct {
	Index  int    `json:"index" jsonschema:"the attachment's number in this list, from 1; view_image takes it"`
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
			text, images, cut := htmlToTextImages(data, maxBodyBytes, len(m.RemoteImages)+1)
			if cut {
				m.Truncated = true
			}
			texts = append(texts, text)
			m.RemoteImages = append(m.RemoteImages, images...)
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

	// want, if not zero, is the Index of the attachment whose content is
	// kept, up to wantLimit bytes (ExtractAttachment).
	want      int
	wantLimit int64
	wantData  []byte
	wantCut   bool
	wantErr   error // reading the attachment failed: wantData is not all of it
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
	if len(p.msg.Attachments) >= MaxAttachments {
		_, _ = io.Copy(io.Discard, e.Body)
		p.msg.Truncated = true
		return
	}
	index := len(p.msg.Attachments) + 1
	var size int64
	if index == p.want {
		data, err := io.ReadAll(io.LimitReader(e.Body, p.wantLimit))
		rest, err2 := io.Copy(io.Discard, e.Body)
		p.wantData, p.wantCut, p.wantErr = data, rest > 0, errors.Join(err, err2)
		size = int64(len(data)) + rest
	} else {
		size, _ = io.Copy(io.Discard, e.Body)
	}
	name, err := (&gomail.AttachmentHeader{Header: e.Header}).Filename()
	if err != nil || name == "" {
		_, params, _ := e.Header.ContentType()
		name = decodeWord(params["name"])
	}
	p.msg.Attachments = append(p.msg.Attachments, Attachment{
		Index:  index,
		Name:   clean(name, maxFilenameRunes),
		Type:   clean(mediaType, maxNameRunes),
		Size:   size,
		Inline: disposition == "inline" || (disposition == "" && e.Header.Get("Content-Id") != ""),
	})
}

// ErrAttachmentNotFound means the message has no attachment with the Index
// asked for.
var ErrAttachmentNotFound = errors.New("the message has no attachment with that index; read_message lists them")

// errAttachmentTooLarge means the attachment is larger than the limit
// ExtractAttachment was given.
var errAttachmentTooLarge = errors.New("attachment too large")

// ErrAttachmentDamaged means an attachment's content could not be read to
// its end: its encoding or the message's structure is broken.
var ErrAttachmentDamaged = errors.New("the attachment is damaged: its content could not be read to the end")

// ExtractAttachment returns attachment number index (Attachment.Index, as
// ParseMessage lists them) and at most limit bytes of its decoded content.
// A larger attachment is an error wrapping errAttachmentTooLarge.
func ExtractAttachment(r io.Reader, index int, limit int64) (Attachment, []byte, error) {
	e, err := gomessage.ReadWithOptions(&io.LimitedReader{R: r, N: MaxMessageBytes}, &gomessage.ReadOptions{MaxHeaderBytes: maxHeaderBytes})
	if err != nil && !gomessage.IsUnknownCharset(err) && !gomessage.IsUnknownEncoding(err) {
		return Attachment{}, nil, ErrUnreadableMessage
	}
	if index < 1 || index > MaxAttachments {
		return Attachment{}, nil, ErrAttachmentNotFound
	}
	p := &parser{msg: &Message{}, want: index, wantLimit: limit}
	p.walk(e, 0)
	if len(p.msg.Attachments) < index {
		return Attachment{}, nil, ErrAttachmentNotFound
	}
	a := p.msg.Attachments[index-1]
	if p.wantCut {
		return a, nil, fmt.Errorf("%w: %d bytes, the limit is %d", errAttachmentTooLarge, a.Size, limit)
	}
	if p.wantErr != nil {
		return a, nil, ErrAttachmentDamaged
	}
	return a, p.wantData, nil
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
