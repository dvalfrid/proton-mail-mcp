package mail

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/dvalfrid/proton-mail-mcp/internal/config"
	"github.com/dvalfrid/proton-mail-mcp/internal/testutil"
)

// inbox adds n messages to INBOX and returns a Selection of all of them.
func inbox(t *testing.T, c *Client, add func(string), n int) Selection {
	t.Helper()
	for range n {
		add("INBOX")
	}
	l, err := c.ListMessages(context.Background(), "INBOX", ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	s := Selection{Folder: "INBOX", UIDValidity: l.UIDValidity}
	for _, m := range l.Messages {
		s.UIDs = append(s.UIDs, m.UID)
	}
	return s
}

func flagged(t *testing.T, c *Client) int {
	t.Helper()
	l, err := c.ListMessages(context.Background(), "INBOX", ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, m := range l.Messages {
		if m.Flagged {
			n++
		}
	}
	return n
}

func TestMarkReadAndFlag(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, srv, _ := newClient(t)
	s := inbox(t, c, func(f string) {
		srv.AddMessage(t, f, message("bob@example.org", "Hi", "Mon, 02 Nov 2026 10:00:00 +0000"))
	}, 3)

	if _, err := c.MarkRead(ctx, s, true); err != nil {
		t.Fatal(err)
	}
	if n := srv.Unseen(t, "INBOX"); n != 0 {
		t.Errorf("unseen = %d after mark read, want 0", n)
	}
	if _, err := c.MarkRead(ctx, Selection{Folder: "INBOX", UIDs: s.UIDs[:1], UIDValidity: s.UIDValidity}, false); err != nil {
		t.Fatal(err)
	}
	if n := srv.Unseen(t, "INBOX"); n != 1 {
		t.Errorf("unseen = %d after marking one unread, want 1", n)
	}

	if _, err := c.Flag(ctx, s, true); err != nil {
		t.Fatal(err)
	}
	if n := flagged(t, c); n != 3 {
		t.Errorf("flagged = %d, want 3", n)
	}
	if _, err := c.Flag(ctx, s, false); err != nil {
		t.Fatal(err)
	}
	if n := flagged(t, c); n != 0 {
		t.Errorf("flagged = %d after unflag, want 0", n)
	}

	if srv.Sent("EXPUNGE") || srv.Sent(`\Deleted`) {
		t.Error("an organize call used EXPUNGE or \\Deleted")
	}
}

// Nothing changes unless every uid is valid and current.
func TestChangeRefusesBeforeTouching(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, srv, _ := newClient(t)
	s := inbox(t, c, func(f string) {
		srv.AddMessage(t, f, message("bob@example.org", "Hi", "Mon, 02 Nov 2026 10:00:00 +0000"))
	}, 2)
	many := make([]uint32, MaxChangeUIDs+1)
	for i := range many {
		many[i] = uint32(i + 1)
	}
	tests := []struct {
		name string
		sel  Selection
		want error
	}{
		{"stale uidvalidity", Selection{Folder: "INBOX", UIDs: s.UIDs, UIDValidity: s.UIDValidity + 1}, ErrUIDValidityChanged},
		{"one uid missing", Selection{Folder: "INBOX", UIDs: append(s.UIDs, 999), UIDValidity: s.UIDValidity}, ErrMessagesMissing},
		{"no uidvalidity", Selection{Folder: "INBOX", UIDs: s.UIDs}, ErrNoUIDValidity},
		{"no uids", Selection{Folder: "INBOX", UIDValidity: s.UIDValidity}, ErrNoUIDs},
		{"uid zero", Selection{Folder: "INBOX", UIDs: []uint32{0}, UIDValidity: s.UIDValidity}, ErrNoUIDs},
		{"too many", Selection{Folder: "INBOX", UIDs: many, UIDValidity: s.UIDValidity}, ErrTooManyUIDs},
	}
	for _, tt := range tests {
		if _, err := c.MarkRead(ctx, tt.sel, true); !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.want)
		}
	}
	if n := srv.Unseen(t, "INBOX"); n != 2 {
		t.Errorf("unseen = %d, want 2: a refused call changed something", n)
	}
	if srv.Sent("STORE") {
		t.Error("a refused call sent STORE")
	}
}

func count(t *testing.T, c *Client, folder string) uint32 {
	t.Helper()
	folders, err := c.Folders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range folders {
		if f.Name == folder {
			return f.Messages
		}
	}
	t.Fatalf("no folder %q", folder)
	return 0
}

func TestMoveAndAddLabel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, srv, _ := newClient(t)
	s := inbox(t, c, func(f string) {
		srv.AddMessage(t, f, message("bob@example.org", "Hi", "Mon, 02 Nov 2026 10:00:00 +0000"))
	}, 2)

	if n, err := c.AddLabel(ctx, s, "Labels/Travel"); err != nil || n != 2 {
		t.Fatalf("add label: %d, %v", n, err)
	}
	if count(t, c, "Labels/Travel") != 2 || count(t, c, "INBOX") != 2 {
		t.Error("a label should leave the messages in INBOX and list them under the label")
	}

	if n, err := c.Move(ctx, s, "Folders/Work"); err != nil || n != 2 {
		t.Fatalf("move: %d, %v", n, err)
	}
	if count(t, c, "INBOX") != 0 || count(t, c, "Folders/Work") != 2 {
		t.Error("the messages didn't move from INBOX to Folders/Work")
	}
	if srv.Sent("EXPUNGE") {
		t.Error("moving used EXPUNGE")
	}
}

// Moves this server won't make are refused before anything changes.
func TestMoveRefusals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, srv, _ := newClient(t)
	s := inbox(t, c, func(f string) {
		srv.AddMessage(t, f, message("bob@example.org", "Hi", "Mon, 02 Nov 2026 10:00:00 +0000"))
	}, 1)
	from := func(folder string) Selection {
		return Selection{Folder: folder, UIDs: s.UIDs, UIDValidity: s.UIDValidity}
	}
	tests := []struct {
		name string
		sel  Selection
		to   string
		want error
	}{
		{"to Trash", s, "Trash", ErrNotAllowed},
		{"to a label", s, "Labels/Travel", ErrNotAllowed},
		{"to Drafts", s, "Drafts", ErrNotAllowed},
		{"to Sent", s, "Sent", ErrNotAllowed},
		{"to All Mail", s, "All Mail", ErrNotAllowed},
		{"to Starred", s, "Starred", ErrNotAllowed},
		{"same folder", s, "INBOX", ErrNotAllowed},
		{"no such folder", s, "Folders/Nope", ErrNoSuchFolder},
		{"out of a label", from("Labels/Travel"), "INBOX", ErrNotAllowed},
		{"out of Drafts", from("Drafts"), "INBOX", ErrNotAllowed},
		{"Sent to INBOX", from("Sent"), "INBOX", ErrNotAllowed},
	}
	for _, tt := range tests {
		if _, err := c.Move(ctx, tt.sel, tt.to); !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.want)
		}
	}
	for _, label := range []string{"Archive", "Folders/Nope", "INBOX"} {
		if _, err := c.AddLabel(ctx, s, label); err == nil {
			t.Errorf("add_label to %q was not refused", label)
		}
	}
	if srv.Sent("MOVE") || srv.Sent("COPY") {
		t.Error("a refused call sent MOVE or COPY")
	}
	if count(t, c, "INBOX") != 1 {
		t.Error("a refused call moved the message")
	}
}

func TestTrash(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, srv, _ := newClient(t)
	s := inbox(t, c, func(f string) {
		srv.AddMessage(t, f, message("bob@example.org", "Hi", "Mon, 02 Nov 2026 10:00:00 +0000"))
	}, 2)
	if n, err := c.Trash(ctx, s); err != nil || n != 2 {
		t.Fatalf("trash: %d, %v", n, err)
	}
	if count(t, c, "INBOX") != 0 || count(t, c, "Trash") != 2 {
		t.Error("the messages didn't move to Trash")
	}
	if srv.Sent("EXPUNGE") || srv.Sent(`\Deleted`) {
		t.Error(`trash used EXPUNGE or \Deleted`)
	}
}

func TestTrashRefusals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, srv, _ := newClient(t)
	for _, f := range []string{"Trash", "Drafts", "All Mail"} {
		srv.AddMessage(t, f, message("bob@example.org", "Hi", "Mon, 02 Nov 2026 10:00:00 +0000"))
	}
	for _, f := range []string{"Trash", "Drafts", "All Mail", "Folders/Nope"} {
		l, _ := c.ListMessages(ctx, f, ListOptions{})
		sel := Selection{Folder: f, UIDs: []uint32{1}, UIDValidity: max(l.UIDValidity, 1)}
		if _, err := c.Trash(ctx, sel); !errors.Is(err, ErrNotAllowed) && !errors.Is(err, ErrNoSuchFolder) {
			t.Errorf("trash from %q: err = %v, want a refusal", f, err)
		}
	}
	if srv.Sent("MOVE") {
		t.Error("a refused trash sent MOVE")
	}
}

// When Bridge can't reach Proton it refuses the change itself (docs/bridge.md
// §10). Every organize call then says so and that the change may be partly
// done, and the next call works again (issue #64).
func TestChangeRefusedByBridge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, srv, _ := newClient(t)
	s := inbox(t, c, func(f string) {
		srv.AddMessage(t, f, message("bob@example.org", "Hi", "Mon, 02 Nov 2026 10:00:00 +0000"))
	}, 2)
	ops := map[string]func() (int, error){
		"mark read": func() (int, error) { return c.MarkRead(ctx, s, true) },
		"flag":      func() (int, error) { return c.Flag(ctx, s, true) },
		"move":      func() (int, error) { return c.Move(ctx, s, "Archive") },
		"label":     func() (int, error) { return c.AddLabel(ctx, s, "Labels/Travel") },
		"trash":     func() (int, error) { return c.Trash(ctx, s) },
	}
	srv.RefuseChanges("failed request (API unreachable)")
	for name, op := range ops {
		_, err := op()
		if !errors.Is(err, ErrProtonUnreachable) || !errors.Is(err, ErrChangeFailed) {
			t.Errorf("%s: err = %v, want ErrProtonUnreachable and ErrChangeFailed", name, err)
		}
	}
	srv.RefuseChanges("some other refusal")
	if _, err := c.MarkRead(ctx, s, true); !errors.Is(err, ErrChangeFailed) || errors.Is(err, ErrProtonUnreachable) {
		t.Errorf("other refusal: err = %v, want ErrChangeFailed only", err)
	}
	if srv.Unseen(t, "INBOX") != 2 || count(t, c, "INBOX") != 2 {
		t.Error("a refused change changed something")
	}
	srv.RefuseChanges("")
	if n, err := c.MarkRead(ctx, s, true); err != nil || n != 2 {
		t.Fatalf("after Proton is back: %d, %v", n, err)
	}
}

// The organize operations stop at MaxChanges messages per Client (issue
// #68). Refused calls that sent nothing to Bridge don't count.
func TestChangeLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := testutil.NewIMAPServer(t)
	tlsCfg, err := TLSConfig(srv.Host, srv.CertFile(t))
	if err != nil {
		t.Fatal(err)
	}
	c := New(Options{
		Addr: srv.Addr(), User: testutil.User, Password: config.Secret(testutil.Password), TLSConfig: tlsCfg,
		Timeout: 5 * time.Second, MaxChanges: 3, Logger: slog.New(slog.DiscardHandler),
	})
	t.Cleanup(func() { _ = c.Close() })
	s := inbox(t, c, func(f string) {
		srv.AddMessage(t, f, message("bob@example.org", "Hi", "Mon, 02 Nov 2026 10:00:00 +0000"))
	}, 2)

	stale := Selection{Folder: "INBOX", UIDs: s.UIDs, UIDValidity: s.UIDValidity + 1}
	for range 5 {
		if _, err := c.MarkRead(ctx, stale, true); !errors.Is(err, ErrUIDValidityChanged) {
			t.Fatalf("stale: %v", err)
		}
	}
	if n, err := c.MarkRead(ctx, s, true); err != nil || n != 2 {
		t.Fatalf("first change: %d, %v (refused calls must not count)", n, err)
	}
	if _, err := c.Flag(ctx, s, true); !errors.Is(err, ErrChangeLimit) {
		t.Fatalf("over the limit: err = %v, want ErrChangeLimit", err)
	}
	if flagged(t, c) != 0 {
		t.Error("a call over the limit changed something")
	}
	one := Selection{Folder: "INBOX", UIDs: s.UIDs[:1], UIDValidity: s.UIDValidity}
	if _, err := c.Flag(ctx, one, true); err != nil {
		t.Fatalf("up to the limit: %v", err)
	}
	if _, err := c.Flag(ctx, one, false); !errors.Is(err, ErrChangeLimit) {
		t.Fatalf("at the limit: err = %v, want ErrChangeLimit", err)
	}
}
