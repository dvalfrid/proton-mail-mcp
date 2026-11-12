package mail

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2/imapclient"
)

// A panic inside an operation becomes ErrInternal, the connection is
// dropped, and the next call reconnects and works (issue #60).
func TestPanicInOperation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, _, logs := newClient(t)
	if _, err := c.Folders(ctx); err != nil {
		t.Fatal(err)
	}
	err := c.do(ctx, func(*imapclient.Client) error {
		var m map[string]int
		m["x"] = 1 //nolint:staticcheck // SA5000: the nil map write is the runtime panic under test
		return nil
	})
	if !errors.Is(err, ErrInternal) {
		t.Fatalf("err = %v, want ErrInternal", err)
	}
	if !strings.Contains(logs.String(), "recovered from a panic") || !strings.Contains(logs.String(), "nil map") {
		t.Errorf("the panic wasn't logged with its cause:\n%s", logs)
	}
	if _, err := c.Folders(ctx); err != nil {
		t.Fatalf("the next call failed: %v", err)
	}
}

// A panic value that isn't a runtime error could hold message content, so
// only its type is logged.
func TestPanicValueNotLogged(t *testing.T) {
	t.Parallel()
	c, _, logs := newClient(t)
	err := c.do(context.Background(), func(*imapclient.Client) error {
		panic("Subject: a secret from the mailbox")
	})
	if !errors.Is(err, ErrInternal) {
		t.Fatalf("err = %v, want ErrInternal", err)
	}
	if strings.Contains(logs.String(), "secret") {
		t.Errorf("the panic value was logged:\n%s", logs)
	}
}
