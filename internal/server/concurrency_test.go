package server

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Many tool calls at once, as an MCP client may send them, all succeed and
// return the right data. They share one IMAP connection behind a mutex; CI
// runs this with the race detector.
func TestConcurrentToolCalls(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	const messages = 10
	for i := 1; i <= messages; i++ {
		e.imap.AddMessage(t, "INBOX", fmt.Sprintf("From: bob@example.org\nTo: alice@example.com\nSubject: Message %d\n"+
			"Date: Mon, 02 Nov 2026 %02d:00:00 +0000\n\nBody %d.\n", i, i, i))
	}

	const workers = 24
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for w := range workers {
		wg.Go(func() {
			if err := concurrentCall(e.session, w, messages); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if n := e.imap.Unseen(t, "INBOX"); n != messages {
		t.Errorf("unseen = %d after concurrent calls, want %d", n, messages)
	}
}

// concurrentCall makes one tool call chosen by worker number and checks the
// result. It returns errors instead of failing the test: it runs on its own
// goroutine.
func concurrentCall(s *mcp.ClientSession, w, messages int) error {
	call := func(name string, args map[string]any, out any) error {
		res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if res.IsError {
			return fmt.Errorf("%s: %s", name, text(res))
		}
		return decode(res, out)
	}

	switch w % 3 {
	case 0:
		var out ListFoldersOutput
		return call("list_folders", map[string]any{}, &out)
	case 1:
		var out ListMessagesOutput
		if err := call("list_messages", map[string]any{"folder": "INBOX", "limit": 5}, &out); err != nil {
			return err
		}
		if len(out.Messages) != 5 || out.Messages[0].Subject != fmt.Sprintf("Message %d", messages) {
			return fmt.Errorf("list_messages = %+v", out.Messages)
		}
	default:
		uid := w%messages + 1
		var out ReadMessageOutput
		if err := call("read_message", map[string]any{"folder": "INBOX", "uid": uid}, &out); err != nil {
			return err
		}
		if want := fmt.Sprintf("Message %d", uid); out.UntrustedContent == nil || out.UntrustedContent.Subject != want {
			return fmt.Errorf("read_message %d: got %+v, want subject %q", uid, out.UntrustedContent, want)
		}
	}
	return nil
}
