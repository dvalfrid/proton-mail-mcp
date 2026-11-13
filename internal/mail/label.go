package mail

import (
	"context"
	"fmt"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// RemoveLabel removes the label s.Folder (a folder under Labels/) from the
// messages s.UIDs in it. The messages stay in their own folder.
//
// Bridge has no other way to remove only a label: it is \Deleted + UID
// EXPUNGE in Labels/X, which Bridge turns into UnlabelMessages(X)
// (docs/bridge.md §4, §6). This is the one EXPUNGE in the server, a narrow
// exception approved by the owner (STANDARDS.md#deletion, issue #55): in
// Trash or Drafts the same commands delete permanently, so expungeInLabel
// checks the folder again on the open connection before each expunge.
func (c *Client) RemoveLabel(ctx context.Context, s Selection) (int, error) {
	if _, err := s.check(); err != nil {
		return 0, err
	}
	mbs, err := c.mailboxes(ctx, s.Folder)
	if err != nil {
		return 0, err
	}
	if !labelOnly(mbs[0]) {
		return 0, fmt.Errorf("%w: %q is not a label; give the label (a folder under Labels/) as the folder, with uids listed from it", ErrNotAllowed, s.Folder)
	}
	return c.change(ctx, s, func(ic *imapclient.Client, set imap.UIDSet) error {
		return expungeInLabel(ic, mbs[0], set)
	})
}

// labelOnly is a user label: under Labels/ and no special-use role. Only
// there does EXPUNGE remove a label rather than delete.
func labelOnly(m mailbox) bool {
	return strings.HasPrefix(m.name, "Labels/") && len(m.name) > len("Labels/") && m.role == ""
}

// expungeInLabel marks set \Deleted in the selected label and expunges
// exactly those uids. It refuses unless the label is a user label and the
// server has UIDPLUS: a plain EXPUNGE would also take messages another
// client had marked.
func expungeInLabel(ic *imapclient.Client, label mailbox, set imap.UIDSet) error {
	if !labelOnly(label) {
		return fmt.Errorf("%w: refusing to expunge outside a label (%q)", ErrNotAllowed, label.name)
	}
	if sel := ic.Mailbox(); sel == nil || sel.Name != label.name {
		return fmt.Errorf("%w: refusing to expunge: %q is not the open folder", ErrNotAllowed, label.name)
	}
	if !ic.Caps().Has(imap.CapUIDPlus) {
		return fmt.Errorf("%w: the server can't expunge single messages (no UIDPLUS)", ErrNotAllowed)
	}
	store := &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}
	if err := ic.Store(set, store, nil).Close(); err != nil {
		return fmt.Errorf("remove label %q: %w", label.name, err)
	}
	//nolint:forbidigo // the one approved EXPUNGE: only in a user label, where Bridge removes the label and deletes nothing (STANDARDS.md#deletion)
	if err := ic.UIDExpunge(set).Close(); err != nil {
		return fmt.Errorf("remove label %q: %w", label.name, err)
	}
	return nil
}
