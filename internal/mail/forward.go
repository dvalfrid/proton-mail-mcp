package mail

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime"
	"strings"
	"time"
	"unicode/utf8"

	gomessage "github.com/emersion/go-message"
	gomail "github.com/emersion/go-message/mail"
)

// Limits on what a forward carries over from the original, the same as the
// outbox's for a draft (internal/outbox): files beyond them are left out
// and named, never cut.
const (
	MaxForwardFiles = 10
	MaxForwardBytes = 20 << 20
)

// Forward is a message to forward as a draft: the original, the owner's
// recipients and note, and whether its attachments go along.
type Forward struct {
	Folder      string
	UID         uint32
	UIDValidity uint32
	// From is the owner's address to forward from; "" picks the one the
	// original was sent to, as replies do (issue #162).
	From        string
	To, Cc, Bcc []string
	Body        string // the owner's note, above the original
	// WithoutAttachments leaves the original's attachments out.
	WithoutAttachments bool
	// Attachments are files from the outbox, read by internal/outbox.
	Attachments []DraftFile
}

// ForwardCreated is a forward draft saved in Drafts.
type ForwardCreated struct {
	Created
	Subject string
	// Forwarded are the original's attachments carried over, by name.
	Forwarded []string
	// LeftOut are the original's attachments not carried over, with why.
	LeftOut []string
}

// CreateForwardDraft saves a forward of the message f names as a new draft
// in Drafts: "Fwd: " and its subject, f.Body, then the original's From,
// Date, Subject, To and Cc and its text, and its attachments (not the
// images embedded in its HTML), up to MaxForwardFiles and MaxForwardBytes
// with the outbox files. The recipients are the owner's, never taken from
// the original. The original is read with EXAMINE and BODY.PEEK, so it
// stays unread. It counts as one change against MaxChanges.
func (c *Client) CreateForwardDraft(ctx context.Context, f Forward) (ForwardCreated, error) {
	if !utf8.ValidString(f.Body) {
		return ForwardCreated{}, fmt.Errorf("%w: the body must be valid UTF-8", ErrBadDraft)
	}
	if len(f.To)+len(f.Cc)+len(f.Bcc) == 0 {
		return ForwardCreated{}, fmt.Errorf("%w: give at least one recipient to forward to", ErrBadDraft)
	}
	mbs, err := c.mailboxes(ctx, f.Folder)
	if err != nil {
		return ForwardCreated{}, err
	}
	own, err := c.ownAddresses(ctx, false)
	if err != nil {
		return ForwardCreated{}, err
	}
	raw, size, err := c.fetchMessage(ctx, f.Folder, f.UID, f.UIDValidity)
	if err != nil {
		return ForwardCreated{}, err
	}
	if size > int64(len(raw)) {
		return ForwardCreated{}, fmt.Errorf("%w: it is %d bytes, the limit is %d", ErrMessageTooLarge, size, c.maxFetch)
	}
	d, out, err := c.forward(raw, f, own, mbs[0].role == "sent")
	if err != nil {
		return ForwardCreated{}, err
	}
	created, err := c.CreateDraft(ctx, d)
	if err != nil {
		return ForwardCreated{}, err
	}
	out.Created = created
	return out, nil
}

// forward builds the draft that forwards raw.
func (c *Client) forward(raw []byte, f Forward, ownAddrs map[string]string, sent bool) (Draft, ForwardCreated, error) {
	e, err := gomessage.Read(bytes.NewReader(raw))
	if e == nil {
		return Draft{}, ForwardCreated{}, fmt.Errorf("%w (%w)", ErrUnreadableMessage, err)
	}
	h := gomail.Header{Header: e.Header}
	m, err := ParseMessage(bytes.NewReader(raw))
	if err != nil {
		return Draft{}, ForwardCreated{}, err
	}

	subject, _ := h.Subject()
	subject = strings.Join(strings.Fields(subject), " ") // one line
	if l := strings.ToLower(subject); !strings.HasPrefix(l, "fwd:") && !strings.HasPrefix(l, "fw:") {
		subject = "Fwd: " + subject
	}
	if utf8.RuneCountInString(subject) > MaxDraftSubjectRunes {
		subject = string([]rune(subject)[:MaxDraftSubjectRunes])
	}

	d := Draft{From: f.From, To: f.To, Cc: f.Cc, Bcc: f.Bcc, Subject: subject}
	if d.From == "" {
		known := map[string]bool{}
		for k := range ownAddrs {
			known[k] = true
		}
		from, _ := h.AddressList("From")
		origTo, _ := h.AddressList("To")
		origCc, _ := h.AddressList("Cc")
		lists := [][]*gomail.Address{origTo, origCc}
		if sent {
			lists = [][]*gomail.Address{from}
		}
		for _, l := range lists {
			if a := firstOwn(l, known); a != "" {
				d.From = a
				break
			}
		}
	}

	note := strings.TrimRight(f.Body, "\r\n")
	d.Body = note + forwarded(m, c.opts.Location, MaxDraftBodyBytes-len(note))

	out := ForwardCreated{Subject: subject}
	d.Attachments = append(d.Attachments, f.Attachments...)
	total := 0
	for _, a := range f.Attachments {
		total += len(a.Data)
	}
	for _, a := range m.Attachments {
		if a.Inline {
			continue // part of the HTML, which is forwarded as text
		}
		name := clean(a.Name, maxNameRunes)
		if name == "" {
			name = fmt.Sprintf("attachment-%d", a.Index)
		}
		if f.WithoutAttachments {
			out.LeftOut = append(out.LeftOut, name+": left out as asked")
			continue
		}
		if len(d.Attachments) >= MaxForwardFiles {
			out.LeftOut = append(out.LeftOut, fmt.Sprintf("%s: a draft carries at most %d files", name, MaxForwardFiles))
			continue
		}
		room := int64(MaxForwardBytes - total)
		_, data, err := ExtractAttachment(bytes.NewReader(raw), a.Index, room)
		switch {
		case errors.Is(err, errAttachmentTooLarge):
			out.LeftOut = append(out.LeftOut, fmt.Sprintf("%s: the files would be larger than %d MiB together", name, MaxForwardBytes>>20))
			continue
		case err != nil:
			out.LeftOut = append(out.LeftOut, name+": it can't be read to its end")
			continue
		}
		total += len(data)
		d.Attachments = append(d.Attachments, DraftFile{Name: name, ContentType: attachmentType(a.Type), Data: data})
		out.Forwarded = append(out.Forwarded, name)
	}
	return d, out, nil
}

// attachmentType is t if it is a valid media type, else a neutral one.
func attachmentType(t string) string {
	if mt, _, err := mime.ParseMediaType(t); err == nil && strings.Count(mt, "/") == 1 {
		return mt
	}
	return "application/octet-stream"
}

// forwarded is the original below the owner's note: its headers as mail
// programs show a forward, then its text, cut to fit in room bytes.
func forwarded(m *Message, loc *time.Location, room int) string {
	var b strings.Builder
	b.WriteString("\n\n---------- Forwarded message ----------\n")
	line := func(k string, v []string) {
		if len(v) > 0 {
			b.WriteString(k + ": " + strings.Join(v, ", ") + "\n")
		}
	}
	line("From", m.From)
	if !m.Date.IsZero() {
		b.WriteString("Date: " + m.Date.In(loc).Format("Mon, 2 Jan 2006 15:04") + "\n")
	}
	b.WriteString("Subject: " + m.Subject + "\n")
	line("To", m.To)
	line("Cc", m.Cc)
	b.WriteString("\n" + strings.TrimRight(m.Body, "\n") + "\n")
	s := b.String()
	if len(s) <= room {
		return s
	}
	if room <= 0 {
		return ""
	}
	s = s[:room]
	for !utf8.ValidString(s) { // don't cut a character in half
		s = s[:len(s)-1]
	}
	return s
}
