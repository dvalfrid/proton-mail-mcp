package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvalfrid/proton-mail-mcp/internal/config"
	"github.com/dvalfrid/proton-mail-mcp/internal/help"
	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
	"github.com/dvalfrid/proton-mail-mcp/internal/testutil"
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
		if !res.IsError || !strings.Contains(text(res), "start Proton Mail Bridge") || !strings.Contains(text(res), help.URL(help.BridgeDown)) {
			t.Errorf("%s: IsError=%v text=%q", tool, res.IsError, text(res))
		}
	}
}

// Every failure the owner can fix tells Claude what to do and links to its
// section of docs/troubleshooting.md (issue #62). TestEveryTopicHasASection
// checks the sections exist.
func TestErrorsLinkToTroubleshooting(t *testing.T) {
	t.Parallel()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := ln.Addr().String()
	_ = ln.Close()
	other := testutil.NewIMAPServer(t)
	tests := []struct {
		name   string
		change func(*mail.Options)
		tool   string
		args   map[string]any
		topic  help.Topic
		action string
	}{
		{"Bridge not running", func(o *mail.Options) { o.Addr = closed }, "list_folders", map[string]any{}, help.BridgeDown, "start Proton Mail Bridge"},
		{"wrong password", func(o *mail.Options) { o.Password = config.Secret("not-it") }, "list_folders", map[string]any{}, help.LoginFailed, "proton-mail-mcp setup"},
		{"another certificate", func(o *mail.Options) { o.Addr = other.Addr() }, "list_folders", map[string]any{}, help.Certificate, "proton-mail-mcp check"},
		{"search timeout", func(o *mail.Options) { o.SearchTimeout = 1 }, "search_messages", map[string]any{"folder": "INBOX", "from": "x"}, help.Timeout, "since or before"},
		{"renumbered folder", nil, "read_message", map[string]any{"folder": "INBOX", "uid": 1, "uidvalidity": 999999}, help.Renumbered, "list it again"},
	}
	for _, tt := range tests {
		e := newEnvWith(t, tt.change)
		e.imap.AddMessage(t, "INBOX", msgFromBob)
		res := e.call(t, tt.tool, tt.args)
		if !res.IsError || !strings.Contains(text(res), tt.action) || !strings.Contains(text(res), "Help: "+help.URL(tt.topic)) {
			t.Errorf("%s: IsError=%v text=%q, want %q and its Help link", tt.name, res.IsError, text(res), tt.action)
		}
	}
}

// Without settings the server still starts, and every tool says to run
// setup, so Claude can tell the owner instead of the server vanishing.
func TestNotSetUpToolsSaySo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := mail.NewUnconfigured(errors.New("PROTON_BRIDGE_USER is not set"), slog.New(slog.DiscardHandler))
	st, ct := mcp.NewInMemoryTransports()
	ss, err := New("test", m).Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	e := &env{session: cs}
	for tool, args := range map[string]map[string]any{
		"list_folders": {}, "list_messages": {"folder": "INBOX"},
		"mark_read": {"folder": "INBOX", "uids": []uint32{1}, "uidvalidity": 1},
	} {
		res := e.call(t, tool, args)
		if !res.IsError || !strings.Contains(text(res), "proton-mail-mcp setup") || !strings.Contains(text(res), help.URL(help.NotSetUp)) {
			t.Errorf("%s: IsError=%v text=%q", tool, res.IsError, text(res))
		}
	}
}
