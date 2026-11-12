package mail

import (
	"context"
	"errors"
	"fmt"
	"slices"

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
