package mail

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	gomail "github.com/emersion/go-message/mail"
)

// maxSentScanned caps the Sent messages whose senders are read to learn the
// owner's addresses: the newest by uid, so a large Sent folder costs one
// bounded FETCH.
const maxSentScanned = 2000

// ErrNotOwnAddress means a draft was asked to come from an address that
// isn't one of the owner's.
var ErrNotOwnAddress = errors.New("not one of the owner's addresses")

// ownAddresses are the owner's addresses, lower-case: the account's, and
// every sender in the newest maxSentScanned messages in Sent, the addresses
// the owner has sent from (issue #162). IMAP can't list a Proton account's
// addresses, so these are the ones known for sure. Read once per Client;
// refresh reads Sent again, for an address first used since.
func (c *Client) ownAddresses(ctx context.Context, refresh bool) (map[string]string, error) {
	c.ownMu.Lock()
	defer c.ownMu.Unlock()
	if c.own != nil && !refresh {
		return c.own, nil
	}
	own := map[string]string{} // lower-case → as written
	if a, err := gomail.ParseAddress(c.opts.User); err == nil {
		own[strings.ToLower(a.Address)] = a.Address
	}
	folders, err := c.roleFolders(ctx)
	if err != nil {
		return nil, err
	}
	if sent := folders["sent"]; sent != "" {
		err := c.do(ctx, func(ic *imapclient.Client) error {
			return sentFrom(ic, sent, own)
		})
		if err != nil {
			return nil, err
		}
	}
	c.own = own
	return own, nil
}

// sentFrom adds the senders of the newest messages in the Sent folder.
func sentFrom(ic *imapclient.Client, sent string, own map[string]string) error {
	if _, err := ic.Select(sent, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return fmt.Errorf("open folder %q: %w", sent, err)
	}
	data, err := ic.UIDSearch(&imap.SearchCriteria{}, nil).Wait()
	if err != nil {
		return fmt.Errorf("list %q: %w", sent, err)
	}
	uids := data.AllUIDs()
	if len(uids) == 0 {
		return nil
	}
	slices.Sort(uids)
	if len(uids) > maxSentScanned {
		uids = uids[len(uids)-maxSentScanned:]
	}
	msgs, err := ic.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{UID: true, Envelope: true}).Collect()
	if err != nil {
		return fmt.Errorf("read senders in %q: %w", sent, err)
	}
	for _, m := range msgs {
		if m.Envelope == nil {
			continue
		}
		for _, a := range m.Envelope.From {
			if addr := a.Addr(); addr != "" && !strings.ContainsFunc(addr, unsafeURLRune) {
				own[strings.ToLower(addr)] = addr
			}
		}
	}
	return nil
}

// OwnAddress checks that from is one of the owner's addresses and returns
// it as the owner wrote it in Sent. An address not known yet is looked for
// once more in Sent before it is refused.
func (c *Client) OwnAddress(ctx context.Context, from string) (string, error) {
	a, err := gomail.ParseAddress(from)
	if err != nil {
		return "", fmt.Errorf("%w: from %q is not an email address", ErrBadDraft, from)
	}
	key := strings.ToLower(a.Address)
	for _, refresh := range []bool{false, true} {
		own, err := c.ownAddresses(ctx, refresh)
		if err != nil {
			return "", err
		}
		if addr, ok := own[key]; ok {
			return addr, nil
		}
		if refresh {
			known := slices.Sorted(maps.Values(own))
			return "", fmt.Errorf("%w: %w: %s. A draft comes only from an address the owner has sent from: %s",
				ErrBadDraft, ErrNotOwnAddress, a.Address, strings.Join(known, ", "))
		}
	}
	return "", nil // not reached
}
