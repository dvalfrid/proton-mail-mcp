package mail

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	gomessage "github.com/emersion/go-message"
	gomail "github.com/emersion/go-message/mail"
)

// Reply is a reply to write as a draft: the message it answers, and the
// owner's text.
type Reply struct {
	Folder      string
	UID         uint32
	UIDValidity uint32
	Body        string
	All         bool // also to the original's To and Cc
}

// ReplyCreated is a reply draft saved in Drafts, with what it was given
// from the original.
type ReplyCreated struct {
	Created
	To, Cc  []string
	Subject string
}

// CreateReplyDraft saves a reply to the message r names as a new draft in
// Drafts: addressed to its sender (Reply-To, else From), with all its other
// recipients if r.All, "Re: " and its subject, r.Body and the original
// quoted. The original is read with EXAMINE and BODY.PEEK, so it stays
// unread. Bridge drops In-Reply-To and References on APPEND (docs/bridge.md
// §7), so Proton doesn't link the draft to the original. It counts as one
// change against MaxChanges.
func (c *Client) CreateReplyDraft(ctx context.Context, r Reply) (ReplyCreated, error) {
	if !utf8.ValidString(r.Body) {
		return ReplyCreated{}, fmt.Errorf("%w: the body must be valid UTF-8", ErrBadDraft)
	}
	raw, _, err := c.fetchMessage(ctx, r.Folder, r.UID, r.UIDValidity)
	if err != nil {
		return ReplyCreated{}, err
	}
	d, err := c.reply(raw, r)
	if err != nil {
		return ReplyCreated{}, err
	}
	created, err := c.CreateDraft(ctx, d)
	if err != nil {
		return ReplyCreated{}, err
	}
	return ReplyCreated{Created: created, To: d.To, Cc: d.Cc, Subject: d.Subject}, nil
}

// reply builds the draft that answers raw.
func (c *Client) reply(raw []byte, r Reply) (Draft, error) {
	e, err := gomessage.Read(bytes.NewReader(raw))
	if e == nil {
		return Draft{}, fmt.Errorf("%w (%w)", ErrUnreadableMessage, err)
	}
	h := gomail.Header{Header: e.Header}
	own := ""
	if a, err := gomail.ParseAddress(c.opts.User); err == nil {
		own = a.Address
	}

	sender, _ := h.AddressList("Reply-To")
	if len(sender) == 0 {
		sender, _ = h.AddressList("From")
	}
	origTo, _ := h.AddressList("To")
	origCc, _ := h.AddressList("Cc")

	seen := map[string]bool{}
	pick := func(lists ...[]*gomail.Address) []string {
		var out []string
		for _, l := range lists {
			for _, a := range l {
				key := strings.ToLower(a.Address)
				if a.Address == "" || seen[key] || strings.EqualFold(a.Address, own) {
					continue
				}
				seen[key] = true
				out = append(out, a.String())
			}
		}
		return out
	}
	to := pick(sender)
	if len(to) == 0 { // the owner's own message: reply to whom it went to
		to = pick(origTo)
	}
	var cc []string
	if r.All {
		cc = pick(origTo, origCc)
	}
	if len(to) == 0 {
		return Draft{}, fmt.Errorf("%w: the message has no address to reply to", ErrBadDraft)
	}

	subject, _ := h.Subject()
	subject = strings.Join(strings.Fields(subject), " ") // one line
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	if utf8.RuneCountInString(subject) > MaxDraftSubjectRunes {
		subject = string([]rune(subject)[:MaxDraftSubjectRunes])
	}

	d := Draft{To: to, Cc: cc, Subject: subject, Body: r.Body}
	if id, err := h.MessageID(); err == nil && msgID(id) {
		d.InReplyTo = id
		refs, _ := h.MsgIDList("References")
		for _, ref := range refs {
			if msgID(ref) {
				d.References = append(d.References, ref)
			}
		}
		d.References = append(d.References, id)
	}

	if m, err := ParseMessage(bytes.NewReader(raw)); err == nil && m.Body != "" {
		text := strings.TrimRight(r.Body, "\r\n")
		d.Body = text + quote(m, c.opts.Location, MaxDraftBodyBytes-len(text))
	}
	return d, nil
}

// msgID is a message ID safe to put back in a header: no spaces, angle
// brackets or line breaks, and not absurdly long.
func msgID(id string) bool {
	return id != "" && len(id) <= 998 && !strings.ContainsAny(id, " \t\r\n<>")
}

// quote is the original after the reply: a line naming its sender and date,
// then its text with "> " before each line, cut to fit in room bytes.
func quote(m *Message, loc *time.Location, room int) string {
	who := "the sender"
	if len(m.From) > 0 {
		who = m.From[0]
	}
	head := "\n\n"
	if !m.Date.IsZero() {
		head += "On " + m.Date.In(loc).Format("Mon, 2 Jan 2006 15:04") + ", " + who + " wrote:\n"
	} else {
		head += who + " wrote:\n"
	}
	var b strings.Builder
	b.WriteString(head)
	for line := range strings.SplitSeq(strings.TrimRight(m.Body, "\n"), "\n") {
		b.WriteString("> " + line + "\n")
	}
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
