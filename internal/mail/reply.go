package mail

import (
	"bytes"
	"context"
	"fmt"
	"maps"
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
	// From is the owner's address to reply from; "" picks the one the
	// original was sent to, or from (issue #162).
	From string
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
	mbs, err := c.mailboxes(ctx, r.Folder)
	if err != nil {
		return ReplyCreated{}, err
	}
	own, err := c.ownAddresses(ctx, false)
	if err != nil {
		return ReplyCreated{}, err
	}
	raw, _, err := c.fetchMessage(ctx, r.Folder, r.UID, r.UIDValidity)
	if err != nil {
		return ReplyCreated{}, err
	}
	d, err := c.reply(raw, r, own, mbs[0].role == "sent")
	if err != nil {
		return ReplyCreated{}, err
	}
	created, err := c.CreateDraft(ctx, d)
	if err != nil {
		return ReplyCreated{}, err
	}
	return ReplyCreated{Created: created, To: shown(d.To), Cc: shown(d.Cc), Subject: d.Subject}, nil
}

// shown is how addresses are told to Claude: "Name <addr>" with the name
// decoded, not the encoded form that goes into the draft's header.
func shown(list []string) []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		if a, err := gomail.ParseAddress(s); err == nil {
			s = formatNameAddr(a.Name, a.Address)
		}
		out = append(out, s)
	}
	return out
}

// reply builds the draft that answers raw. ownAddrs are the owner's
// addresses (ownAddresses): never replied to, and the reply comes from the
// one the original was sent from or to (issue #162). sent says raw is in
// the Sent folder: then it is the owner's own message, whichever address it
// was sent from (issue #148).
func (c *Client) reply(raw []byte, r Reply, ownAddrs map[string]string, sent bool) (Draft, error) {
	e, err := gomessage.Read(bytes.NewReader(raw))
	if e == nil {
		return Draft{}, fmt.Errorf("%w (%w)", ErrUnreadableMessage, err)
	}
	h := gomail.Header{Header: e.Header}
	from, _ := h.AddressList("From")
	own := map[string]bool{} // the owner's addresses, never replied to
	for k := range ownAddrs {
		own[k] = true
	}
	if a, err := gomail.ParseAddress(c.opts.User); err == nil {
		own[strings.ToLower(a.Address)] = true
	}
	known := maps.Clone(own) // addresses OwnAddress accepts as a sender
	if sent {
		for _, a := range from {
			own[strings.ToLower(a.Address)] = true
		}
	}

	sender, _ := h.AddressList("Reply-To")
	if len(sender) == 0 {
		sender = from
	}
	origTo, _ := h.AddressList("To")
	origCc, _ := h.AddressList("Cc")

	seen := map[string]bool{}
	pick := func(lists ...[]*gomail.Address) []string {
		var out []string
		for _, l := range lists {
			for _, a := range l {
				key := strings.ToLower(a.Address)
				if a.Address == "" || seen[key] || own[key] {
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

	d := Draft{From: r.From, To: to, Cc: cc, Subject: subject, Body: r.Body}
	if d.From == "" {
		// The owner's address the original came from (their own message)
		// or was sent to, as Proton Mail's own reply does.
		for _, l := range [][]*gomail.Address{from, origTo, origCc} {
			if a := firstOwn(l, known); a != "" {
				d.From = a
				break
			}
		}
	}
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

// firstOwn is the first address in l that is the owner's, or "".
func firstOwn(l []*gomail.Address, own map[string]bool) string {
	for _, a := range l {
		if own[strings.ToLower(a.Address)] {
			return a.Address
		}
	}
	return ""
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
