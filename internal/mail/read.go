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

// ListMessages returns the newest messages in folder, newest first. The
// folder is opened read-only: listing never marks anything as read.
func (c *Client) ListMessages(ctx context.Context, folder string, opts ListOptions) ([]Summary, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}
	limit = min(limit, MaxListLimit)

	var out []Summary
	err := c.do(ctx, func(ic *imapclient.Client) error {
		if _, err := ic.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
			return fmt.Errorf("open folder %q: %w", folder, err)
		}

		criteria := &imap.SearchCriteria{}
		if opts.UnreadOnly {
			criteria.NotFlag = []imap.Flag{imap.FlagSeen}
		}
		data, err := ic.UIDSearch(criteria, nil).Wait()
		if err != nil {
			return fmt.Errorf("search folder %q: %w", folder, err)
		}
		uids := data.AllUIDs()
		if len(uids) == 0 {
			return nil
		}
		slices.Sort(uids)
		if len(uids) > limit {
			uids = uids[len(uids)-limit:]
		}

		msgs, err := ic.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{
			UID: true, Envelope: true, Flags: true, InternalDate: true, RFC822Size: true,
		}).Collect()
		if err != nil {
			return fmt.Errorf("fetch messages in %q: %w", folder, err)
		}
		for _, m := range msgs {
			out = append(out, summarize(m))
		}
		slices.SortFunc(out, func(a, b Summary) int { return cmp.Compare(b.UID, a.UID) })
		return nil
	})
	return out, err
}

// ErrMessageNotFound means the folder has no message with the given UID.
var ErrMessageNotFound = errors.New("message not found")

// ReadMessage fetches one message by UID and parses it with ParseMessage.
// Like ListMessages it never marks anything as read: the folder is opened
// read-only and the body is fetched with BODY.PEEK. At most MaxMessageBytes
// are fetched; a larger message is parsed from that prefix and marked
// truncated.
func (c *Client) ReadMessage(ctx context.Context, folder string, uid uint32) (*Message, error) {
	var (
		raw   []byte
		size  int64
		found bool
	)
	section := &imap.FetchItemBodySection{Peek: true, Partial: &imap.SectionPartial{Size: c.maxFetch}}
	err := c.do(ctx, func(ic *imapclient.Client) error {
		if _, err := ic.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
			return fmt.Errorf("open folder %q: %w", folder, err)
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
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
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
