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

// FindContacts finds the people whose name and address together contain
// every word of query, in any order (case, accents and extra spaces
// ignored). It looks at the newest messages in Sent, INBOX and Archive: the
// To, Cc and Bcc of the owner's own messages (whom they wrote to) and the
// From of the others (who wrote to them), never the owner's own addresses.
// People the owner wrote to come first, then by how many messages and how
// recent. Only ENVELOPE is read (a database read in Bridge), from folders
// opened read-only, and each folder is kept in memory until it changes.
func (c *Client) FindContacts(ctx context.Context, query string) (Contacts, error) {
	words := strings.Fields(fold(query))
	if len(words) == 0 {
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
	add := func(e contactEntry) {
		key := strings.ToLower(e.addr)
		if own[key] != "" {
			return
		}
		p := people[key]
		if p == nil {
			p = &Contact{Address: e.addr}
			people[key] = p
		}
		if e.sent {
			p.Sent++
		} else {
			p.Received++
		}
		if e.date.After(p.Last) || p.Last.IsZero() {
			p.Last = e.date
			if e.name != "" {
				p.Name = e.name
			}
		} else if p.Name == "" {
			p.Name = e.name
		}
	}
	for _, folder := range []string{folders["sent"], "INBOX", folders["archive"]} {
		if folder == "" || out.Scanned[folder] > 0 {
			continue
		}
		var box contactBox
		err := c.do(ctx, func(ic *imapclient.Client) error {
			var err error
			box, err = c.contactBox(ic, folder, own)
			return err
		})
		if err != nil {
			return Contacts{}, err
		}
		out.Scanned[folder] = box.scanned
		for _, e := range box.entries {
			add(e)
		}
	}
	for _, p := range people {
		if matches(words, fold(p.Name)+" "+fold(p.Address)) {
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

// matches reports whether every word is in s.
func matches(words []string, s string) bool {
	for _, w := range words {
		if !strings.Contains(s, w) {
			return false
		}
	}
	return true
}

// contactEntry is one address in one message: whom the owner wrote to
// (sent) or who wrote to them.
type contactEntry struct {
	name, addr string
	date       time.Time
	sent       bool
}

// contactBox is what one folder gave when it was last read, valid while
// its UIDVALIDITY, UIDNEXT and message count stay the same.
type contactBox struct {
	validity, next, count uint32
	scanned               int
	entries               []contactEntry
}

// contactBox returns the addresses in the newest maxContactScan messages in
// folder, from the cache when the folder hasn't changed. A message from one
// of own is the owner's: its recipients count as written to. It runs under
// c.mu (within do), which also guards the cache.
func (c *Client) contactBox(ic *imapclient.Client, folder string, own map[string]string) (contactBox, error) {
	st, err := ic.Status(folder, &imap.StatusOptions{UIDValidity: true, UIDNext: true, NumMessages: true}).Wait()
	if err != nil {
		return contactBox{}, fmt.Errorf("status of %q: %w", folder, err)
	}
	box := contactBox{validity: st.UIDValidity, next: uint32(st.UIDNext)}
	if st.NumMessages != nil {
		box.count = *st.NumMessages
	}
	if old, ok := c.contactBoxes[folder]; ok && old.validity == box.validity && old.next == box.next && old.count == box.count {
		return old, nil
	}
	envs, err := newestEnvelopes(ic, folder)
	if err != nil {
		return contactBox{}, err
	}
	box.scanned = len(envs)
	keep := func(a imap.Address, date time.Time, sent bool) {
		if addr := a.Addr(); addr != "" && !strings.ContainsFunc(addr, unsafeURLRune) {
			box.entries = append(box.entries, contactEntry{name: clean(a.Name, maxNameRunes), addr: addr, date: date, sent: sent})
		}
	}
	for _, e := range envs {
		mine := false
		for _, a := range e.From {
			mine = mine || own[strings.ToLower(a.Addr())] != ""
		}
		if !mine {
			for _, a := range e.From {
				keep(a, e.Date, false)
			}
			continue
		}
		for _, l := range [][]imap.Address{e.To, e.Cc, e.Bcc} {
			for _, a := range l {
				keep(a, e.Date, true)
			}
		}
	}
	if c.contactBoxes == nil {
		c.contactBoxes = map[string]contactBox{}
	}
	c.contactBoxes[folder] = box
	return box, nil
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
