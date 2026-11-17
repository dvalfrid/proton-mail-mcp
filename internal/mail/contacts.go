package mail

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// Limits on finding contacts (issue #187): the newest messages read per
// folder, and the people returned.
const (
	maxContactScan    = 3000
	MaxContactResults = 25
)

// Contact is a person found in the headers of the owner's mail. Bridge has
// no contacts (docs/bridge.md §11), so this is what the owner wrote to and
// heard from. The name is written by whoever sent the mail: untrusted.
type Contact struct {
	Name     string    `json:"name,omitempty" jsonschema:"the name shown with the address; written by senders, untrusted"`
	Address  string    `json:"address"`
	Sent     int       `json:"sent" jsonschema:"messages the owner sent to this address (To, Cc or Bcc), among those read"`
	Received int       `json:"received" jsonschema:"messages from this address in INBOX, among those read"`
	Last     time.Time `json:"last" jsonschema:"the newest of those messages"`
}

// Contacts is the answer to FindContacts.
type Contacts struct {
	People []Contact `json:"people"`
	// Scanned is how many messages were read, per folder.
	Scanned map[string]int `json:"scanned"`
}

// FindContacts finds the people whose name or address contains query
// (case, accents and extra spaces ignored), in the To, Cc and Bcc of the
// newest messages in Sent and the From of the newest in INBOX, never the
// owner's own addresses. People the owner wrote to come first, then by how
// many messages and how recent. Only ENVELOPE is read (a database read in
// Bridge), from folders opened read-only.
func (c *Client) FindContacts(ctx context.Context, query string) (Contacts, error) {
	q := fold(query)
	if q == "" {
		return Contacts{}, fmt.Errorf("give a name or part of an address to look for")
	}
	own, err := c.ownAddresses(ctx, false)
	if err != nil {
		return Contacts{}, err
	}
	folders, err := c.roleFolders(ctx)
	if err != nil {
		return Contacts{}, err
	}
	people := map[string]*Contact{}
	out := Contacts{Scanned: map[string]int{}}
	add := func(name, addr string, when time.Time, sent bool) {
		key := strings.ToLower(addr)
		if addr == "" || own[key] != "" || strings.ContainsFunc(addr, unsafeURLRune) {
			return
		}
		p := people[key]
		if p == nil {
			p = &Contact{Address: addr}
			people[key] = p
		}
		if sent {
			p.Sent++
		} else {
			p.Received++
		}
		if when.After(p.Last) || p.Last.IsZero() {
			p.Last = when
			if n := clean(name, maxNameRunes); n != "" {
				p.Name = n
			}
		} else if p.Name == "" {
			p.Name = clean(name, maxNameRunes)
		}
	}
	scan := []struct {
		folder string
		sent   bool
	}{{folders["sent"], true}, {"INBOX", false}}
	for _, s := range scan {
		if s.folder == "" {
			continue
		}
		var envs []*imap.Envelope
		err := c.do(ctx, func(ic *imapclient.Client) error {
			var err error
			envs, err = newestEnvelopes(ic, s.folder)
			return err
		})
		if err != nil {
			return Contacts{}, err
		}
		out.Scanned[s.folder] = len(envs)
		for _, e := range envs {
			if s.sent {
				for _, l := range [][]imap.Address{e.To, e.Cc, e.Bcc} {
					for _, a := range l {
						add(a.Name, a.Addr(), e.Date, true)
					}
				}
			} else {
				for _, a := range e.From {
					add(a.Name, a.Addr(), e.Date, false)
				}
			}
		}
	}
	for _, p := range people {
		if strings.Contains(fold(p.Name), q) || strings.Contains(fold(p.Address), q) {
			out.People = append(out.People, *p)
		}
	}
	slices.SortFunc(out.People, func(a, b Contact) int {
		wrote := func(c Contact) int { return min(c.Sent, 1) }
		if c := cmp.Compare(wrote(b), wrote(a)); c != 0 {
			return c // anyone the owner wrote to before anyone they didn't
		}
		if c := cmp.Compare(b.Sent+b.Received, a.Sent+a.Received); c != 0 {
			return c
		}
		return b.Last.Compare(a.Last)
	})
	if len(out.People) > MaxContactResults {
		out.People = out.People[:MaxContactResults]
	}
	return out, nil
}

// newestEnvelopes reads the envelopes of the newest maxContactScan
// messages in folder, by UID.
func newestEnvelopes(ic *imapclient.Client, folder string) ([]*imap.Envelope, error) {
	if _, err := ic.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return nil, fmt.Errorf("open folder %q: %w", folder, err)
	}
	data, err := ic.UIDSearch(&imap.SearchCriteria{}, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("list %q: %w", folder, err)
	}
	uids := data.AllUIDs()
	if len(uids) == 0 {
		return nil, nil
	}
	slices.Sort(uids)
	if len(uids) > maxContactScan {
		uids = uids[len(uids)-maxContactScan:]
	}
	msgs, err := ic.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{UID: true, Envelope: true}).Collect()
	if err != nil {
		return nil, fmt.Errorf("read addresses in %q: %w", folder, err)
	}
	out := make([]*imap.Envelope, 0, len(msgs))
	for _, m := range msgs {
		if m.Envelope != nil {
			out = append(out, m.Envelope)
		}
	}
	return out, nil
}

// accents folds the accented letters common in names to their base
// letter, so "asa" finds "Åsa" and "jose" finds "José".
var accents = strings.NewReplacer(
	"å", "a", "ä", "a", "à", "a", "á", "a", "â", "a", "ã", "a",
	"ö", "o", "ø", "o", "ó", "o", "ò", "o", "ô", "o", "õ", "o",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"ü", "u", "ú", "u", "ù", "u", "û", "u",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ç", "c", "ñ", "n", "æ", "ae", "ß", "ss",
)

// fold is s for matching: lower case, accents folded, runs of space as one.
func fold(s string) string {
	return strings.Join(strings.Fields(accents.Replace(strings.ToLower(s))), " ")
}
