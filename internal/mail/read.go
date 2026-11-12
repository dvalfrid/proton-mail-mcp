package mail

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// Limits on what a listing returns.
const (
	DefaultListLimit = 20
	MaxListLimit     = 100
	maxSubjectRunes  = 300
	maxNameRunes     = 120
)

// Folder is a mailbox as Claude sees it.
type Folder struct {
	Name     string `json:"name"`
	Role     string `json:"role,omitempty" jsonschema:"special use: inbox, drafts, sent, trash, spam, archive or all"`
	Messages uint32 `json:"messages"`
	Unread   uint32 `json:"unread"`
}

// Summary is one message in a listing. Subject and From are written by the
// sender: they are untrusted data.
type Summary struct {
	UID     uint32    `json:"uid" jsonschema:"message id within its folder, used by other tools"`
	Date    time.Time `json:"date"`
	From    string    `json:"from"`
	Subject string    `json:"subject"`
	Unread  bool      `json:"unread"`
	Flagged bool      `json:"flagged"`
	Size    int64     `json:"size_bytes"`
}

// ListOptions selects which messages ListMessages returns.
type ListOptions struct {
	Limit      int  // newest first; DefaultListLimit if zero, capped at MaxListLimit
	UnreadOnly bool // only messages without \Seen
}

// Folders lists all folders with message and unread counts.
func (c *Client) Folders(ctx context.Context) ([]Folder, error) {
	var folders []Folder
	err := c.do(ctx, func(ic *imapclient.Client) error {
		list, err := ic.List("", "*", nil).Collect()
		if err != nil {
			return fmt.Errorf("list folders: %w", err)
		}
		for _, mb := range list {
			if slices.Contains(mb.Attrs, imap.MailboxAttrNoSelect) || slices.Contains(mb.Attrs, imap.MailboxAttrNonExistent) {
				continue
			}
			f := Folder{Name: mb.Mailbox, Role: role(mb)}
			st, err := ic.Status(mb.Mailbox, &imap.StatusOptions{NumMessages: true, NumUnseen: true}).Wait()
			if err != nil {
				return fmt.Errorf("status of %q: %w", mb.Mailbox, err)
			}
			if st.NumMessages != nil {
				f.Messages = *st.NumMessages
			}
			if st.NumUnseen != nil {
				f.Unread = *st.NumUnseen
			}
			folders = append(folders, f)
		}
		return nil
	})
	return folders, err
}

func role(mb *imap.ListData) string {
	if strings.EqualFold(mb.Mailbox, "INBOX") {
		return "inbox"
	}
	roles := map[imap.MailboxAttr]string{
		imap.MailboxAttrDrafts:  "drafts",
		imap.MailboxAttrSent:    "sent",
		imap.MailboxAttrTrash:   "trash",
		imap.MailboxAttrJunk:    "spam",
		imap.MailboxAttrArchive: "archive",
		imap.MailboxAttrAll:     "all",
	}
	for _, a := range mb.Attrs {
		if r, ok := roles[a]; ok {
			return r
		}
	}
	return ""
}

// Listing is the result of ListMessages and SearchMessages.
type Listing struct {
	// UIDValidity is the folder's UIDVALIDITY when the listing was made. A UID
	// is only meaningful together with it: Bridge renumbers every message,
	// with a new UIDVALIDITY, when it resyncs from scratch (docs/bridge.md §3).
	UIDValidity uint32
	Messages    []Summary // newest first
}

// ListMessages returns the newest messages in folder, newest first. The
// folder is opened read-only: listing never marks anything as read.
func (c *Client) ListMessages(ctx context.Context, folder string, opts ListOptions) (Listing, error) {
	criteria := &imap.SearchCriteria{}
	if opts.UnreadOnly {
		criteria.NotFlag = []imap.Flag{imap.FlagSeen}
	}
	return c.summaries(ctx, folder, criteria, days{}, opts.Limit, c.opts.Timeout)
}

// SearchOptions are the criteria of SearchMessages, combined with AND. Text
// criteria match case-insensitive substrings, as IMAP SEARCH defines them.
type SearchOptions struct {
	From    string // From header contains
	To      string // To header contains
	Subject string // Subject contains
	Body    string // body text contains
	// Since and Before are days in the owner's time zone (Options.Location):
	// only their year, month and day count. A message matches by the date
	// its Summary shows.
	Since      time.Time // dated on or after this day
	Before     time.Time // dated before this day
	UnreadOnly bool
	Limit      int // newest first; DefaultListLimit if zero, capped at MaxListLimit
}

// ErrNoSearchCriteria means SearchMessages got nothing to search for.
var ErrNoSearchCriteria = errors.New("no search criteria")

// SearchMessages runs an IMAP SEARCH in folder and returns the newest
// matches. The terms are sent as IMAP search arguments (quoted or as
// literals by the client library), never spliced into a command line. The
// folder is opened read-only.
func (c *Client) SearchMessages(ctx context.Context, folder string, opts SearchOptions) (Listing, error) {
	criteria := &imap.SearchCriteria{}
	// Bridge compares SINCE and BEFORE with UTC midnight, on the received
	// date (docs/bridge.md, section 2). So IMAP is asked for a day more on
	// each side, and the matches are then cut to the owner's days by the
	// date that is shown.
	var within days
	if !opts.Since.IsZero() {
		within.from = c.midnight(opts.Since)
		criteria.Since = utcDay(within.from).AddDate(0, 0, -1)
	}
	if !opts.Before.IsZero() {
		within.until = c.midnight(opts.Before)
		criteria.Before = utcDay(within.until).AddDate(0, 0, 2)
	}
	for _, h := range []struct{ key, value string }{{"From", opts.From}, {"To", opts.To}, {"Subject", opts.Subject}} {
		if h.value != "" {
			criteria.Header = append(criteria.Header, imap.SearchCriteriaHeaderField{Key: h.key, Value: h.value})
		}
	}
	if opts.Body != "" {
		criteria.Body = []string{opts.Body}
	}
	if opts.UnreadOnly {
		criteria.NotFlag = []imap.Flag{imap.FlagSeen}
	}
	if len(criteria.Header) == 0 && len(criteria.Body) == 0 && len(criteria.NotFlag) == 0 &&
		criteria.Since.IsZero() && criteria.Before.IsZero() {
		return Listing{}, ErrNoSearchCriteria
	}
	return c.summaries(ctx, folder, criteria, within, opts.Limit, c.opts.SearchTimeout)
}

// days is a span of time: from (inclusive) to until (exclusive), either
// zero for no bound.
type days struct{ from, until time.Time }

func (d days) contain(t time.Time) bool {
	return (d.from.IsZero() || !t.Before(d.from)) && (d.until.IsZero() || t.Before(d.until))
}

// midnight is the start of day's date in the owner's time zone.
func (c *Client) midnight(day time.Time) time.Time {
	y, m, d := day.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, c.opts.Location)
}

// utcDay is the UTC date t falls on, at midnight.
func utcDay(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// summaries searches folder and returns the limit newest matches, newest
// first.
//
// "Newest" means by INTERNALDATE (the date received), not by UID: Bridge's
// initial sync gives the newest messages the lowest UIDs, and Bridge has no
// SORT extension (docs/bridge.md, sections 1 and 3). So the dates of all
// matches are fetched, which Bridge serves from its database without
// loading messages, and sorted here. Only the newest are fetched in full.
// Equal dates fall back to the higher UID first, so the order is stable.
func (c *Client) summaries(ctx context.Context, folder string, criteria *imap.SearchCriteria, within days, limit int, timeout time.Duration) (Listing, error) {
	if limit <= 0 {
		limit = DefaultListLimit
	}
	limit = min(limit, MaxListLimit)

	var out Listing
	err := c.doFor(ctx, timeout, func(ic *imapclient.Client) error {
		sel, err := ic.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
		if err != nil {
			return fmt.Errorf("open folder %q: %w", folder, err)
		}
		out.UIDValidity = sel.UIDValidity
		data, err := ic.UIDSearch(criteria, nil).Wait()
		if err != nil {
			return fmt.Errorf("search folder %q: %w", folder, err)
		}
		uids := data.AllUIDs()
		if len(uids) == 0 {
			return nil
		}

		dated, err := ic.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{UID: true, InternalDate: true, Envelope: true}).Collect()
		if err != nil {
			return fmt.Errorf("fetch dates in %q: %w", folder, err)
		}
		dated = slices.DeleteFunc(dated, func(m *imapclient.FetchMessageBuffer) bool { return !within.contain(shownDate(m)) })
		if len(dated) == 0 {
			return nil
		}
		newestFirst := func(a, b *imapclient.FetchMessageBuffer) int {
			if c := sortDate(b).Compare(sortDate(a)); c != 0 {
				return c
			}
			return cmp.Compare(b.UID, a.UID)
		}
		slices.SortFunc(dated, newestFirst)
		dated = dated[:min(limit, len(dated))]
		rank := make(map[imap.UID]int, len(dated))
		keep := make([]imap.UID, 0, len(dated))
		for i, m := range dated {
			rank[m.UID] = i
			keep = append(keep, m.UID)
		}

		// Responses can arrive in any order; Collect matches them by UID.
		msgs, err := ic.Fetch(imap.UIDSetNum(keep...), &imap.FetchOptions{
			UID: true, Envelope: true, Flags: true, InternalDate: true, RFC822Size: true,
		}).Collect()
		if err != nil {
			return fmt.Errorf("fetch messages in %q: %w", folder, err)
		}
		slices.SortFunc(msgs, func(a, b *imapclient.FetchMessageBuffer) int { return cmp.Compare(rank[a.UID], rank[b.UID]) })
		for _, m := range msgs {
			out.Messages = append(out.Messages, summarize(m, c.opts.Location))
		}
		return nil
	})
	return out, err
}

// sortDate is the date a message is ordered by: the earlier of its received
// date (INTERNALDATE) and its Date header.
//
//   - The received date alone isn't enough: on a real Bridge, a batch of
//     synced messages carried the sync time as their received date, years
//     after they were sent.
//   - The Date header alone isn't safe: the sender writes it, and a future
//     date would keep a message at the top.
//
// Taking the earlier of the two means a sender can make a message look
// older, never newer.
func sortDate(m *imapclient.FetchMessageBuffer) time.Time {
	d := m.InternalDate
	if env := m.Envelope; env != nil && !env.Date.IsZero() && (d.IsZero() || env.Date.Before(d)) {
		d = env.Date
	}
	return d
}

// ErrMessageNotFound means the folder has no message with the given UID.
var ErrMessageNotFound = errors.New("message not found")

// ErrUIDValidityChanged means the folder's UIDVALIDITY is no longer the one
// the caller's UID came with: the folder was renumbered (Bridge resynced),
// and the UID may now name a different message.
var ErrUIDValidityChanged = errors.New("the folder was renumbered since its uids were listed (Bridge resynced); list it again")

// ReadMessage fetches one message by UID and parses it with ParseMessage.
// Like ListMessages it never marks anything as read: the folder is opened
// read-only and the body is fetched with BODY.PEEK. At most MaxMessageBytes
// are fetched; a larger message is parsed from that prefix and marked
// truncated.
//
// If uidValidity is not zero, it must equal the folder's current
// UIDVALIDITY, or ErrUIDValidityChanged is returned before any message is
// fetched.
func (c *Client) ReadMessage(ctx context.Context, folder string, uid, uidValidity uint32) (*Message, error) {
	raw, size, err := c.fetchMessage(ctx, folder, uid, uidValidity)
	if err != nil {
		return nil, err
	}
	m, err := ParseMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	if size > c.maxFetch {
		m.Truncated = true
	}
	if !m.Date.IsZero() {
		m.Date = m.Date.In(c.opts.Location)
	}
	return m, nil
}

// fetchMessage fetches at most maxFetch bytes of one raw message with
// BODY.PEEK from a folder opened read-only, and returns them with the
// message's full size. It checks uidValidity as ReadMessage describes.
func (c *Client) fetchMessage(ctx context.Context, folder string, uid, uidValidity uint32) ([]byte, int64, error) {
	var (
		raw     []byte
		size    int64
		found   bool
		stale   bool
		current uint32
	)
	section := &imap.FetchItemBodySection{Peek: true, Partial: &imap.SectionPartial{Size: c.maxFetch}}
	err := c.do(ctx, func(ic *imapclient.Client) error {
		sel, err := ic.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
		if err != nil {
			return fmt.Errorf("open folder %q: %w", folder, err)
		}
		// Reported after do returns, like a missing UID: do treats any other
		// error as a broken connection.
		if uidValidity != 0 && sel.UIDValidity != uidValidity {
			stale, current = true, sel.UIDValidity
			return nil
		}
		msgs, err := ic.Fetch(imap.UIDSetNum(imap.UID(uid)), &imap.FetchOptions{
			UID: true, RFC822Size: true, BodySection: []*imap.FetchItemBodySection{section},
		}).Collect()
		if err != nil {
			return fmt.Errorf("fetch message %d in %q: %w", uid, folder, err)
		}
		// A UID that doesn't exist gives an empty result, not an IMAP error.
		// It's reported after do returns: do treats any other error as a
		// broken connection.
		if len(msgs) == 0 {
			return nil
		}
		found = true
		raw, size = msgs[0].FindBodySection(section), msgs[0].RFC822Size
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	if stale {
		return nil, 0, fmt.Errorf("%w (uidvalidity %d, now %d)", ErrUIDValidityChanged, uidValidity, current)
	}
	if !found {
		return nil, 0, ErrMessageNotFound
	}
	return raw, size, nil
}

// shownDate is the date a listing shows for a message: its Date header, or
// the received date (INTERNALDATE) if it has none.
func shownDate(m *imapclient.FetchMessageBuffer) time.Time {
	if env := m.Envelope; env != nil && !env.Date.IsZero() {
		return env.Date
	}
	return m.InternalDate
}

// summarize makes the Summary of m, with its date in loc.
func summarize(m *imapclient.FetchMessageBuffer, loc *time.Location) Summary {
	s := Summary{
		UID:     uint32(m.UID),
		Date:    shownDate(m).In(loc),
		Unread:  !slices.Contains(m.Flags, imap.FlagSeen),
		Flagged: slices.Contains(m.Flags, imap.FlagFlagged),
		Size:    m.RFC822Size,
	}
	if env := m.Envelope; env != nil {
		s.Subject = clean(env.Subject, maxSubjectRunes)
		if len(env.From) > 0 {
			s.From = formatAddress(env.From[0])
		}
	}
	return s
}

func formatAddress(a imap.Address) string {
	return formatNameAddr(a.Name, a.Addr())
}

// formatNameAddr renders a sender-controlled address as "Name <addr>",
// cleaned and capped.
func formatNameAddr(name, addr string) string {
	addr = clean(addr, maxNameRunes)
	name = clean(name, maxNameRunes)
	if name == "" || name == addr {
		return addr
	}
	return name + " <" + addr + ">"
}

// clean makes sender-controlled header text safe to show: control characters
// (including newlines, which could fake extra lines in the output) become
// spaces, whitespace is collapsed, and the text is capped at max runes.
func clean(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.Is(unicode.Cf, r):
			// Bidi overrides and zero-width characters: a filename like
			// "invoice<U+202E>fdp.exe" would read as "invoice exe.pdf"
			// (issue #74). cleanBody drops them too.
			return -1
		case unicode.IsControl(r) || r == ' ' || r == ' ':
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

// ErrMessageTooLarge means a message is larger than the bytes fetched
// (MaxMessageBytes), so its attachments can't be read whole.
var ErrMessageTooLarge = errors.New("the message is too large to read its attachments whole")

// Attachment returns attachment number index (Attachment.Index, as
// ReadMessage lists them) and its decoded content, at most limit bytes: a
// larger one is an error wrapping errAttachmentTooLarge, with the
// Attachment filled in. It never marks the message read, and checks
// uidValidity like ReadMessage. A message larger than what is fetched is
// refused, so a cut attachment is never returned as if it were whole.
func (c *Client) Attachment(ctx context.Context, folder string, uid, uidValidity uint32, index int, limit int64) (Attachment, []byte, error) {
	raw, size, err := c.fetchMessage(ctx, folder, uid, uidValidity)
	if err != nil {
		return Attachment{}, nil, err
	}
	if size > c.maxFetch {
		return Attachment{}, nil, fmt.Errorf("%w: it is %d bytes, the limit is %d", ErrMessageTooLarge, size, c.maxFetch)
	}
	return ExtractAttachment(bytes.NewReader(raw), index, limit)
}
