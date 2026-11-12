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
	return c.summaries(ctx, folder, criteria, opts.Limit, c.opts.Timeout)
}

// SearchOptions are the criteria of SearchMessages, combined with AND. Text
// criteria match case-insensitive substrings, as IMAP SEARCH defines them.
type SearchOptions struct {
	From       string    // From header contains
	To         string    // To header contains
	Subject    string    // Subject contains
	Body       string    // body text contains
	Since      time.Time // received on or after this day (only the date counts)
	Before     time.Time // received before this day (only the date counts)
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
	criteria := &imap.SearchCriteria{Since: opts.Since, Before: opts.Before}
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
	return c.summaries(ctx, folder, criteria, opts.Limit, c.opts.SearchTimeout)
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
func (c *Client) summaries(ctx context.Context, folder string, criteria *imap.SearchCriteria, limit int, timeout time.Duration) (Listing, error) {
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
			out.Messages = append(out.Messages, summarize(m))
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
		return nil, err
	}
	if stale {
		return nil, fmt.Errorf("%w (uidvalidity %d, now %d)", ErrUIDValidityChanged, uidValidity, current)
	}
	if !found {
		return nil, ErrMessageNotFound
	}
	m, err := ParseMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	if size > c.maxFetch {
		m.Truncated = true
	}
	return m, nil
}

func summarize(m *imapclient.FetchMessageBuffer) Summary {
	s := Summary{
		UID:     uint32(m.UID),
		Date:    m.InternalDate,
		Unread:  !slices.Contains(m.Flags, imap.FlagSeen),
		Flagged: slices.Contains(m.Flags, imap.FlagFlagged),
		Size:    m.RFC822Size,
	}
	if env := m.Envelope; env != nil {
		if !env.Date.IsZero() {
			s.Date = env.Date
		}
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
