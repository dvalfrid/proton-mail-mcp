package mail

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	netmail "net/mail"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	gomail "github.com/emersion/go-message/mail"
)

// Limits on a draft. A draft is for the owner to read before it goes
// anywhere, so these only keep a fooled assistant from making absurd ones.
const (
	MaxDraftRecipients   = 50
	MaxDraftSubjectRunes = 500
	MaxDraftBodyBytes    = 256 << 10
)

// ErrBadDraft means the draft was refused before anything reached Bridge.
var ErrBadDraft = errors.New("the draft was not created")

// Draft is a new plain-text email to save in Drafts. Nothing sends it.
type Draft struct {
	To, Cc, Bcc []string
	Subject     string
	Body        string
	// InReplyTo and References thread a reply (message IDs without <>).
	// Bridge drops them on APPEND (docs/bridge.md §7); they are set anyway.
	InReplyTo  string
	References []string
}

// Created is a draft saved in Drafts.
type Created struct {
	Folder      string
	UID         uint32 // 0 if Bridge didn't say (no APPENDUID)
	UIDValidity uint32
}

// CreateDraft saves d as a new draft in the folder with the \Drafts
// attribute. Bridge turns the appended message into a Proton draft
// (docs/bridge.md §7). It counts as one change against MaxChanges, so
// instructions hidden in an email can't fill Drafts.
func (c *Client) CreateDraft(ctx context.Context, d Draft) (Created, error) {
	raw, err := c.buildDraft(d, time.Now())
	if err != nil {
		return Created{}, err
	}
	folders, err := c.roleFolders(ctx)
	if err != nil {
		return Created{}, err
	}
	drafts := folders["drafts"]
	if drafts == "" {
		return Created{}, fmt.Errorf("%w: no folder is marked as Drafts", ErrNoSuchFolder)
	}
	if err := c.reserve(1); err != nil {
		return Created{}, err
	}
	var out Created
	err = c.do(ctx, func(ic *imapclient.Client) error {
		out, err = appendDraft(ic, drafts, raw)
		return err
	})
	if err != nil {
		return Created{}, changeFailed(err)
	}
	return out, nil
}

// Replaced is a draft replaced by UpdateDraft.
type Replaced struct {
	Created
	// OldTrashed says the old draft was moved to Trash. If not, it is
	// still in Drafts and Kept says why.
	OldTrashed bool
	Kept       string
}

// UpdateDraft replaces the draft uid in Drafts with d. IMAP can't change a
// message, so the new version is appended and the old one moved to Trash,
// where the owner can restore it from (docs/bridge.md §7). Bridge before
// 3.23.1 deleted a draft moved to Trash permanently, so the old draft is
// moved only when Bridge says, in IMAP ID, that it is 3.23.1 or later;
// otherwise it stays in Drafts and Kept says why. Never EXPUNGE. It counts
// as one change against MaxChanges.
func (c *Client) UpdateDraft(ctx context.Context, uid, uidValidity uint32, d Draft) (Replaced, error) {
	raw, err := c.buildDraft(d, time.Now())
	if err != nil {
		return Replaced{}, err
	}
	folders, err := c.roleFolders(ctx)
	if err != nil {
		return Replaced{}, err
	}
	drafts, trash := folders["drafts"], folders["trash"]
	switch {
	case drafts == "":
		return Replaced{}, fmt.Errorf("%w: no folder is marked as Drafts", ErrNoSuchFolder)
	case trash == "":
		return Replaced{}, fmt.Errorf("%w: no folder is marked as Trash", ErrNoSuchFolder)
	}
	var out Replaced
	sel := Selection{Folder: drafts, UIDs: []uint32{uid}, UIDValidity: uidValidity}
	_, err = c.change(ctx, sel, func(ic *imapclient.Client, set imap.UIDSet) error {
		safe, why, err := c.draftTrashSafe(ic)
		if err != nil {
			return err // the connection, before anything changed
		}
		created, err := appendDraft(ic, drafts, raw)
		if err != nil {
			return err
		}
		out.Created = created
		if !safe {
			out.Kept = "the old draft is still in Drafts: " + why + ". The owner can delete the old draft in Proton Mail, or update Bridge."
			return nil
		}
		if _, err := ic.Move(set, trash).Wait(); err != nil {
			return fmt.Errorf("the new draft is saved (uid %d), but the old one wasn't moved to Trash: %w", created.UID, err)
		}
		out.OldTrashed = true
		return nil
	})
	if err != nil {
		return Replaced{}, err
	}
	return out, nil
}

// draftTrashSafe asks Bridge its version and says whether a draft moved to
// Trash stays there; if not, why says why. err is only a broken connection.
func (c *Client) draftTrashSafe(ic *imapclient.Client) (safe bool, why string, err error) {
	rel, err := c.bridgeVersion(ic)
	var imapErr *imap.Error
	switch {
	case err != nil && !errors.Is(err, errVersionUnknown) && !errors.As(err, &imapErr):
		return false, "", err
	case err != nil:
		return false, "Proton Mail Bridge didn't say which version it is, and Bridge before " +
			trashDraftsSince.String() + " deletes a draft moved to Trash permanently", nil
	case !rel.atLeast(trashDraftsSince):
		return false, "this Proton Mail Bridge is version " + rel.String() + ", and Bridge before " +
			trashDraftsSince.String() + " deletes a draft moved to Trash permanently", nil
	}
	return true, "", nil
}

// roleFolders maps each special-use role (drafts, trash, …) to its folder.
func (c *Client) roleFolders(ctx context.Context) (map[string]string, error) {
	all, err := c.allMailboxes(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for name, m := range all {
		if m.role != "" {
			out[m.role] = name
		}
	}
	return out, nil
}

// appendDraft APPENDs raw to the Drafts folder and returns the new uid
// from APPENDUID.
func appendDraft(ic *imapclient.Client, folder string, raw []byte) (Created, error) {
	cmd := ic.Append(folder, int64(len(raw)), &imap.AppendOptions{Flags: []imap.Flag{imap.FlagDraft, imap.FlagSeen}})
	if _, err := cmd.Write(raw); err != nil {
		_ = cmd.Close() // the write error is the one to report
		return Created{}, fmt.Errorf("save the draft: %w", err)
	}
	if err := cmd.Close(); err != nil {
		return Created{}, fmt.Errorf("save the draft: %w", err)
	}
	data, err := cmd.Wait()
	if err != nil {
		return Created{}, fmt.Errorf("save the draft: %w", err)
	}
	return Created{Folder: folder, UID: uint32(data.UID), UIDValidity: data.UIDValidity}, nil
}

// bridgeRelease is a Proton Mail Bridge version.
type bridgeRelease struct{ major, minor, patch int }

// trashDraftsSince is the first Bridge that keeps a draft moved to Trash
// instead of deleting it permanently (changelog 3.23.1, BRIDGE-488;
// docs/bridge.md §6).
var trashDraftsSince = bridgeRelease{3, 23, 1}

func (r bridgeRelease) atLeast(o bridgeRelease) bool {
	if r.major != o.major {
		return r.major > o.major
	}
	if r.minor != o.minor {
		return r.minor > o.minor
	}
	return r.patch >= o.patch
}

func (r bridgeRelease) String() string { return fmt.Sprintf("%d.%d.%d", r.major, r.minor, r.patch) }

// errVersionUnknown means Bridge's version couldn't be read.
var errVersionUnknown = errors.New("the Bridge version is unknown")

// bridgeName is the name Bridge gives in IMAP ID.
const bridgeName = "Proton Mail Bridge"

// askBridgeVersion asks the server its name and version with IMAP ID
// (RFC 2971), over the verified TLS connection. Bridge answers
// "name" "Proton Mail Bridge" "version" "03.27.01" (docs/bridge.md §1).
func askBridgeVersion(ic *imapclient.Client) (bridgeRelease, error) {
	if !ic.Caps().Has(imap.CapID) {
		return bridgeRelease{}, errVersionUnknown
	}
	data, err := ic.ID(nil).Wait()
	if err != nil {
		return bridgeRelease{}, err
	}
	if data == nil || data.Name != bridgeName {
		return bridgeRelease{}, errVersionUnknown
	}
	return parseBridgeVersion(data.Version)
}

// parseBridgeVersion reads "03.27.01" (gluon pads each part) or "3.27.1".
func parseBridgeVersion(s string) (bridgeRelease, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return bridgeRelease{}, errVersionUnknown
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return bridgeRelease{}, errVersionUnknown
		}
		n[i] = v
	}
	return bridgeRelease{n[0], n[1], n[2]}, nil
}

// buildDraft checks d and writes it as an RFC 822 message: UTF-8 plain
// text, quoted-printable, From the account's address.
func (c *Client) buildDraft(d Draft, now time.Time) ([]byte, error) {
	to, err := addresses("to", d.To)
	if err != nil {
		return nil, err
	}
	cc, err := addresses("cc", d.Cc)
	if err != nil {
		return nil, err
	}
	bcc, err := addresses("bcc", d.Bcc)
	if err != nil {
		return nil, err
	}
	switch n := len(to) + len(cc) + len(bcc); {
	case len(to) == 0:
		return nil, fmt.Errorf("%w: give at least one address in to", ErrBadDraft)
	case n > MaxDraftRecipients:
		return nil, fmt.Errorf("%w: %d recipients, at most %d", ErrBadDraft, n, MaxDraftRecipients)
	}
	switch {
	case !utf8.ValidString(d.Subject) || !utf8.ValidString(d.Body):
		return nil, fmt.Errorf("%w: the subject and body must be valid UTF-8", ErrBadDraft)
	case strings.ContainsAny(d.Subject, "\r\n"):
		return nil, fmt.Errorf("%w: the subject must be one line", ErrBadDraft)
	case utf8.RuneCountInString(d.Subject) > MaxDraftSubjectRunes:
		return nil, fmt.Errorf("%w: the subject is longer than %d characters", ErrBadDraft, MaxDraftSubjectRunes)
	case len(d.Body) > MaxDraftBodyBytes:
		return nil, fmt.Errorf("%w: the body is larger than %d KiB", ErrBadDraft, MaxDraftBodyBytes>>10)
	}

	var h gomail.Header
	if from, err := netmail.ParseAddress(c.opts.User); err == nil {
		// Bridge uses the account's default address if this isn't one of
		// the account's own (docs/bridge.md §7).
		h.SetAddressList("From", []*gomail.Address{{Name: from.Name, Address: from.Address}})
	}
	h.SetAddressList("To", to)
	if len(cc) > 0 {
		h.SetAddressList("Cc", cc)
	}
	if len(bcc) > 0 {
		h.SetAddressList("Bcc", bcc)
	}
	h.SetSubject(d.Subject)
	if msgID(d.InReplyTo) {
		h.SetMsgIDList("In-Reply-To", []string{d.InReplyTo})
	}
	if refs := slices.DeleteFunc(slices.Clone(d.References), func(id string) bool { return !msgID(id) }); len(refs) > 0 {
		h.SetMsgIDList("References", refs)
	}
	h.SetDate(now)
	if err := h.GenerateMessageID(); err != nil {
		return nil, fmt.Errorf("make a Message-ID: %w", err)
	}
	h.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
	h.Set("Content-Transfer-Encoding", "quoted-printable")

	var buf bytes.Buffer
	w, err := gomail.CreateSingleInlineWriter(&buf, h)
	if err != nil {
		return nil, fmt.Errorf("write the draft: %w", err)
	}
	body := strings.ReplaceAll(d.Body, "\r\n", "\n")
	if _, err := io.WriteString(w, body); err != nil {
		return nil, fmt.Errorf("write the draft: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("write the draft: %w", err)
	}
	return buf.Bytes(), nil
}

// addresses parses each entry as one address ("a@example.com" or
// "Name <a@example.com>"). A list in one entry or a line break is refused,
// so nothing can add a header or a hidden recipient. The error names the
// entry by number, never by its text, which may come from an email.
func addresses(field string, in []string) ([]*gomail.Address, error) {
	out := make([]*gomail.Address, 0, len(in))
	for i, s := range in {
		if strings.ContainsAny(s, "\r\n") {
			return nil, fmt.Errorf("%w: address %d in %s has a line break", ErrBadDraft, i+1, field)
		}
		a, err := netmail.ParseAddress(strings.TrimSpace(s))
		if err != nil { // its text quotes the address, so it isn't passed on
			return nil, fmt.Errorf("%w: address %d in %s is not one email address (give one address per entry)", ErrBadDraft, i+1, field)
		}
		out = append(out, &gomail.Address{Name: a.Name, Address: a.Address})
	}
	return out, nil
}
