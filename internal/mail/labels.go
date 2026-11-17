package mail

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// labelPrefix is where Bridge shows the owner's labels: a label is a mailbox
// Labels/<name>, and a labelled message is in it too (docs/bridge.md §4).
const labelPrefix = "Labels/"

// maxLabelBoxes caps the label mailboxes a listing looks in.
const maxLabelBoxes = 100

// msgKey identifies one message across Bridge's mailboxes without loading
// it: gluon keeps the envelope, received date and size in its database, and
// the same message has the same three in every mailbox it is in. Bridge sets
// Message-Id from the message's own header, else from Proton's ids.
type msgKey struct {
	id   string
	date int64
	size int64
}

func keyOf(m *imapclient.FetchMessageBuffer) (msgKey, bool) {
	if m.Envelope == nil || m.Envelope.MessageID == "" {
		return msgKey{}, false
	}
	return msgKey{id: m.Envelope.MessageID, date: m.InternalDate.Unix(), size: m.RFC822Size}, true
}

// labelBox is what one label mailbox held when it was last read, valid while
// its UIDVALIDITY, UIDNEXT and message count stay the same.
type labelBox struct {
	validity, next, count uint32
	keys                  map[msgKey]struct{}
}

// labelsOf finds the labels of the listed messages: which Labels/ mailboxes
// hold each of them. Each label mailbox is read once (ENVELOPE, INTERNALDATE
// and RFC822.SIZE: database reads in Bridge, no message is loaded) and kept
// until it changes. It stops starting on new label mailboxes at budget, or
// after maxLabelBoxes, and then reports complete = false. It runs within the
// listing's operation, under c.mu, which also guards the cache. Mailboxes
// are opened read-only.
func (c *Client) labelsOf(ic *imapclient.Client, msgs []*imapclient.FetchMessageBuffer, budget time.Time) (labels map[imap.UID][]string, complete bool, err error) {
	want := map[msgKey][]imap.UID{}
	for _, m := range msgs {
		if k, ok := keyOf(m); ok {
			want[k] = append(want[k], m.UID)
		}
	}
	if len(want) == 0 {
		return nil, true, nil
	}
	list, err := ic.List("", labelPrefix+"*", nil).Collect()
	if err != nil {
		return nil, false, fmt.Errorf("list labels: %w", err)
	}
	var boxes []string
	for _, mb := range list {
		if strings.HasPrefix(mb.Mailbox, labelPrefix) && !slices.Contains(mb.Attrs, imap.MailboxAttrNoSelect) && !slices.Contains(mb.Attrs, imap.MailboxAttrNonExistent) {
			boxes = append(boxes, mb.Mailbox)
		}
	}
	slices.Sort(boxes)
	complete = len(boxes) <= maxLabelBoxes
	boxes = boxes[:min(len(boxes), maxLabelBoxes)]
	if c.labelBoxes == nil {
		c.labelBoxes = map[string]labelBox{}
	}

	labels = map[imap.UID][]string{}
	for _, name := range boxes {
		if !time.Now().Before(budget) {
			complete = false
			break
		}
		box, err := c.labelBox(ic, name)
		var imapErr *imap.Error
		if errors.As(err, &imapErr) {
			// Bridge refused this one (removed since LIST, say): leave it out.
			complete = false
			continue
		}
		if err != nil {
			return nil, false, err
		}
		for k := range box.keys {
			for _, uid := range want[k] {
				labels[uid] = append(labels[uid], clean(strings.TrimPrefix(name, labelPrefix), maxNameRunes))
			}
		}
	}
	return labels, complete, nil
}

// labelBox returns what the label mailbox name holds, from the cache when it
// hasn't changed.
func (c *Client) labelBox(ic *imapclient.Client, name string) (labelBox, error) {
	st, err := ic.Status(name, &imap.StatusOptions{UIDValidity: true, UIDNext: true, NumMessages: true}).Wait()
	if err != nil {
		return labelBox{}, fmt.Errorf("status of %q: %w", name, err)
	}
	box := labelBox{validity: st.UIDValidity, next: uint32(st.UIDNext), keys: map[msgKey]struct{}{}}
	if st.NumMessages != nil {
		box.count = *st.NumMessages
	}
	if old, ok := c.labelBoxes[name]; ok && old.validity == box.validity && old.next == box.next && old.count == box.count {
		return old, nil
	}
	if box.count > 0 {
		if _, err := ic.Select(name, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
			return labelBox{}, fmt.Errorf("open label %q: %w", name, err)
		}
		all, err := ic.Fetch(imapUIDAll(), fetchForLabels()).Collect()
		if err != nil {
			return labelBox{}, fmt.Errorf("fetch label %q: %w", name, err)
		}
		for _, m := range all {
			if k, ok := keyOf(m); ok {
				box.keys[k] = struct{}{}
			}
		}
	}
	c.labelBoxes[name] = box
	return box, nil
}

// imapUIDAll is every message in the selected mailbox: UID 1:*.
func imapUIDAll() imap.UIDSet { return imap.UIDSet{imap.UIDRange{Start: 1, Stop: 0}} }

// fetchForLabels is what keyOf needs, all from Bridge's database.
func fetchForLabels() *imap.FetchOptions {
	return &imap.FetchOptions{UID: true, Envelope: true, InternalDate: true, RFC822Size: true}
}
