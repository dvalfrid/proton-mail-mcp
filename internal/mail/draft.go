package mail

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	netmail "net/mail"
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
	all, err := c.allMailboxes(ctx)
	if err != nil {
		return Created{}, err
	}
	var folder string
	for name, m := range all {
		if m.role == "drafts" {
			folder = name
		}
	}
	if folder == "" {
		return Created{}, fmt.Errorf("%w: no folder is marked as Drafts", ErrNoSuchFolder)
	}
	if err := c.reserve(1); err != nil {
		return Created{}, err
	}
	var out Created
	err = c.do(ctx, func(ic *imapclient.Client) error {
		cmd := ic.Append(folder, int64(len(raw)), &imap.AppendOptions{Flags: []imap.Flag{imap.FlagDraft, imap.FlagSeen}})
		if _, err := cmd.Write(raw); err != nil {
			_ = cmd.Close() // the write error is the one to report
			return fmt.Errorf("save the draft: %w", err)
		}
		if err := cmd.Close(); err != nil {
			return fmt.Errorf("save the draft: %w", err)
		}
		data, err := cmd.Wait()
		if err != nil {
			return fmt.Errorf("save the draft: %w", err)
		}
		out = Created{Folder: folder, UID: uint32(data.UID), UIDValidity: data.UIDValidity}
		return nil
	})
	if err != nil {
		return Created{}, changeFailed(err)
	}
	return out, nil
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
