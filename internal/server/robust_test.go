package server

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
)

// A tool that panics returns an error, and the server keeps serving
// (issue #60). Without recoverPanics this test process dies.
func TestPanicInToolIsAnError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := New("test", nil)
	mcp.AddTool(s, &mcp.Tool{Name: "boom"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, struct{}, error) {
		var m map[string]int
		m["x"] = 1 //nolint:staticcheck // SA5000: the nil map write is the runtime panic under test
		return nil, struct{}{}, nil
	})
	st, ct := mcp.NewInMemoryTransports()
	ss, err := s.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	for range 2 {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "boom", Arguments: map[string]any{}})
		if err == nil && (res == nil || !res.IsError) {
			t.Fatal("a panicking tool returned success")
		}
		if err != nil && !strings.Contains(err.Error(), "still running") {
			t.Errorf("err = %v, want the internal-error message", err)
		}
	}
	if _, err := cs.ListTools(ctx, nil); err != nil {
		t.Fatalf("the server stopped answering after a panic: %v", err)
	}
}

// With Bridge not running, a tool call fails with an error that says so,
// and nothing crashes.
func TestToolCallWithoutBridge(t *testing.T) {
	t.Parallel()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := ln.Addr().String()
	_ = ln.Close()
	e := newEnvWith(t, func(o *mail.Options) { o.Addr = closed })
	for tool, args := range map[string]map[string]any{"list_folders": {}, "list_messages": {"folder": "INBOX"}} {
		res := e.call(t, tool, args)
		if !res.IsError || !strings.Contains(text(res), "is Bridge running") {
			t.Errorf("%s: IsError=%v text=%q", tool, res.IsError, text(res))
		}
	}
}
