package mail

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// MaxChangeUIDs caps the messages one organize call may change.
const MaxChangeUIDs = 50

// Errors from the organize operations. All of them are returned before any
// message is changed.
var (
	ErrNoUIDs          = errors.New("no uids given")
	ErrTooManyUIDs     = fmt.Errorf("more than %d uids", MaxChangeUIDs)
	ErrNoUIDValidity   = errors.New("uidvalidity is required to change messages")
	ErrMessagesMissing = errors.New("not every uid is in the folder")
)

// Selection names the messages an organize operation changes: uids in one
// folder, valid only for that folder's uidValidity (docs/bridge.md §3).
type Selection struct {
	Folder      string
	UIDs        []uint32
	UIDValidity uint32
}

func (s Selection) check() ([]uint32, error) {
	uids := slices.Clone(s.UIDs)
	slices.Sort(uids)
	uids = slices.Compact(uids)
	switch {
	case len(uids) == 0 || slices.Contains(uids, 0):
		return nil, ErrNoUIDs
	case len(uids) > MaxChangeUIDs:
		return nil, ErrTooManyUIDs
	case s.UIDValidity == 0:
		return nil, ErrNoUIDValidity
	}
	return uids, nil
}

// change opens the folder read-write and runs fn on the selected messages,
// returning how many there were (duplicates counted once). It runs fn only if
// the folder's UIDVALIDITY still matches and every uid exists; otherwise it
// returns an error and nothing is changed. A uid that doesn't exist would
// otherwise be skipped silently by STORE, COPY and MOVE.
func (c *Client) change(ctx context.Context, s Selection, fn func(*imapclient.Client, imap.UIDSet) error) (int, error) {
	uids, err := s.check()
	if err != nil {
		return 0, err
	}
	set := imap.UIDSet{}
	for _, u := range uids {
		set.AddNum(imap.UID(u))
	}
	var (
		current uint32
		missing []uint32
	)
	err = c.do(ctx, func(ic *imapclient.Client) error {
		sel, err := ic.Select(s.Folder, nil).Wait()
		if err != nil {
			return fmt.Errorf("open folder %q: %w", s.Folder, err)
		}
		// Stale and missing are reported after do returns: do treats any
		// error that isn't the server's as a broken connection.
		if current = sel.UIDValidity; current != s.UIDValidity {
			return nil
		}
		data, err := ic.UIDSearch(&imap.SearchCriteria{UID: []imap.UIDSet{set}}, nil).Wait()
		if err != nil {
			return fmt.Errorf("look up uids in %q: %w", s.Folder, err)
		}
		found := data.AllUIDs()
		for _, u := range uids {
			if !slices.Contains(found, imap.UID(u)) {
				missing = append(missing, u)
			}
		}
		if len(missing) > 0 {
			return nil
		}
		return fn(ic, set)
	})
	switch {
	case err != nil:
		return 0, err
	case current != s.UIDValidity:
		return 0, fmt.Errorf("%w (uidvalidity %d, now %d)", ErrUIDValidityChanged, s.UIDValidity, current)
	case len(missing) > 0:
		return 0, fmt.Errorf("%w: %v", ErrMessagesMissing, missing)
	}
	return len(uids), nil
}

// MarkRead sets (read) or clears (!read) \Seen. Bridge syncs it to Proton
// (docs/bridge.md §5).
func (c *Client) MarkRead(ctx context.Context, s Selection, read bool) (int, error) {
	return c.setFlag(ctx, s, imap.FlagSeen, read)
}

// Flag sets or clears \Flagged, which Bridge maps to Proton's Starred
// (docs/bridge.md §4).
func (c *Client) Flag(ctx context.Context, s Selection, flagged bool) (int, error) {
	return c.setFlag(ctx, s, imap.FlagFlagged, flagged)
}

func (c *Client) setFlag(ctx context.Context, s Selection, flag imap.Flag, on bool) (int, error) {
	op := imap.StoreFlagsDel
	if on {
		op = imap.StoreFlagsAdd
	}
	return c.change(ctx, s, func(ic *imapclient.Client, set imap.UIDSet) error {
		if err := ic.Store(set, &imap.StoreFlags{Op: op, Silent: true, Flags: []imap.Flag{flag}}, nil).Close(); err != nil {
			return fmt.Errorf("set %s in %q: %w", flag, s.Folder, err)
		}
		return nil
	})
}

// ErrNoSuchFolder means a folder named in an organize call doesn't exist.
var ErrNoSuchFolder = errors.New("no such folder")

// ErrNotAllowed means the move or label is one this server won't make. The
// error says which tool to use instead, where there is one.
var ErrNotAllowed = errors.New("not allowed")

type mailbox struct{ name, role string }

func (m mailbox) label() bool { return strings.HasPrefix(m.name, "Labels/") }

// ordinary is a folder a message lives in and can be moved to: INBOX,
// Archive, Spam or a user folder. Not Trash (trash_message), Drafts, Sent,
// a label, Starred, All Mail or Scheduled.
func (m mailbox) ordinary() bool {
	return m.role == "inbox" || m.role == "archive" || m.role == "spam" || strings.HasPrefix(m.name, "Folders/")
}

// allMailboxes lists the selectable folders with their roles, by name.
func (c *Client) allMailboxes(ctx context.Context) (map[string]mailbox, error) {
	all := map[string]mailbox{}
	err := c.do(ctx, func(ic *imapclient.Client) error {
		list, err := ic.List("", "*", nil).Collect()
		if err != nil {
			return fmt.Errorf("list folders: %w", err)
		}
		for _, mb := range list {
			if !slices.Contains(mb.Attrs, imap.MailboxAttrNoSelect) && !slices.Contains(mb.Attrs, imap.MailboxAttrNonExistent) {
				all[mb.Mailbox] = mailbox{name: mb.Mailbox, role: role(mb)}
			}
		}
		return nil
	})
	return all, err
}

// mailboxes looks up the named folders with their roles.
func (c *Client) mailboxes(ctx context.Context, names ...string) ([]mailbox, error) {
	all, err := c.allMailboxes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]mailbox, 0, len(names))
	for _, n := range names {
		m, ok := all[n]
		if !ok {
			return nil, fmt.Errorf("%w: %q; call list_folders to see the names", ErrNoSuchFolder, n)
		}
		out = append(out, m)
	}
	return out, nil
}

// movable says whether messages may move from src to dst. Bridge refuses
// some moves itself (docs/bridge.md §4); the rest keep each tool to one job.
func movable(src, dst mailbox) error {
	switch {
	case src.name == dst.name:
		return fmt.Errorf("%w: the messages are already in %q", ErrNotAllowed, dst.name)
	case dst.role == "trash":
		return fmt.Errorf("%w: to move messages to Trash, use trash_message", ErrNotAllowed)
	case dst.label():
		return fmt.Errorf("%w: %q is a label; use add_label", ErrNotAllowed, dst.name)
	case !dst.ordinary():
		return fmt.Errorf("%w: messages can't be moved to %q; move them to INBOX, Archive, Spam or a folder under Folders/", ErrNotAllowed, dst.name)
	case src.label():
		return fmt.Errorf("%w: moving out of a label also removes the label; that isn't supported yet", ErrNotAllowed)
	case !src.ordinary() && src.role != "trash" && src.role != "sent":
		return fmt.Errorf("%w: messages can't be moved out of %q", ErrNotAllowed, src.name)
	case src.role == "sent" && dst.role == "inbox":
		return fmt.Errorf("%w: Proton Mail Bridge refuses moves between Sent and INBOX", ErrNotAllowed)
	}
	return nil
}

// Move moves messages to another folder with UID MOVE. On Bridge that files
// them there on Proton and removes them from the source; nothing is deleted.
func (c *Client) Move(ctx context.Context, s Selection, to string) (int, error) {
	if _, err := s.check(); err != nil {
		return 0, err
	}
	mbs, err := c.mailboxes(ctx, s.Folder, to)
	if err != nil {
		return 0, err
	}
	if err := movable(mbs[0], mbs[1]); err != nil {
		return 0, err
	}
	return c.change(ctx, s, func(ic *imapclient.Client, set imap.UIDSet) error {
		if _, err := ic.Move(set, to).Wait(); err != nil {
			return fmt.Errorf("move from %q to %q: %w", s.Folder, to, err)
		}
		return nil
	})
}

// AddLabel adds a label with UID COPY to Labels/X. On Bridge the messages
// stay where they are and get the label (docs/bridge.md §4).
func (c *Client) AddLabel(ctx context.Context, s Selection, label string) (int, error) {
	if _, err := s.check(); err != nil {
		return 0, err
	}
	mbs, err := c.mailboxes(ctx, s.Folder, label)
	if err != nil {
		return 0, err
	}
	if !mbs[1].label() {
		return 0, fmt.Errorf("%w: %q is not a label; labels are the folders under Labels/", ErrNotAllowed, label)
	}
	if mbs[0].name == label {
		return 0, fmt.Errorf("%w: the messages already have the label %q", ErrNotAllowed, label)
	}
	return c.change(ctx, s, func(ic *imapclient.Client, set imap.UIDSet) error {
		if _, err := ic.Copy(set, label).Wait(); err != nil {
			return fmt.Errorf("label %q in %q: %w", label, s.Folder, err)
		}
		return nil
	})
}

// Trash moves messages to the folder with the \Trash attribute, found by
// attribute rather than by name, with UID MOVE. Nothing is deleted: the
// owner can restore them from Trash until it is emptied (docs/bridge.md §6).
// Never STORE \Deleted + EXPUNGE: in Trash and Drafts that is a permanent
// delete on Bridge.
func (c *Client) Trash(ctx context.Context, s Selection) (int, error) {
	if _, err := s.check(); err != nil {
		return 0, err
	}
	all, err := c.allMailboxes(ctx)
	if err != nil {
		return 0, err
	}
	src, ok := all[s.Folder]
	if !ok {
		return 0, fmt.Errorf("%w: %q; call list_folders to see the names", ErrNoSuchFolder, s.Folder)
	}
	var trash string
	for name, m := range all {
		if m.role == "trash" {
			trash = name
		}
	}
	switch {
	case trash == "":
		return 0, fmt.Errorf("%w: no folder is marked as Trash", ErrNoSuchFolder)
	case src.role == "trash":
		return 0, fmt.Errorf("%w: the messages are already in Trash", ErrNotAllowed)
	case src.role == "drafts":
		// Bridge before 3.23.1 deleted drafts moved to Trash permanently.
		return 0, fmt.Errorf("%w: drafts can't be moved to Trash yet", ErrNotAllowed)
	case src.role == "all" || src.name == "Scheduled":
		return 0, fmt.Errorf("%w: Proton Mail Bridge refuses moves out of %q; trash the messages from their own folder", ErrNotAllowed, src.name)
	}
	return c.change(ctx, s, func(ic *imapclient.Client, set imap.UIDSet) error {
		if _, err := ic.Move(set, trash).Wait(); err != nil {
			return fmt.Errorf("move from %q to Trash: %w", s.Folder, err)
		}
		return nil
	})
}
