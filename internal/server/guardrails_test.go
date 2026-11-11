package server

// Tests for the security guardrails in STANDARDS.md. Each one fails if its
// guardrail breaks. Don't weaken one to make a change pass: stop and ask.

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dvalfrid/proton-mail-mcp/internal/testutil"
)

var update = flag.Bool("update", false, "rewrite testdata/tools.golden.json")

// The exact set of tools and their schemas. Adding or changing a tool is a
// visible diff in review. Update with: go test ./internal/server -update
func TestToolSnapshot(t *testing.T) {
	e := newEnv(t)
	res, err := e.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(res.Tools, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')

	golden := filepath.Join("testdata", "tools.golden.json")
	if *update {
		if err := os.MkdirAll("testdata", 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run: go test ./internal/server -update)", err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != string(got) {
		t.Errorf("tool list changed. If intended, run: go test ./internal/server -update, and review the diff.\n%s", got)
	}
}

// Read tools must say so, so clients can tell them apart from tools that
// change the mailbox.
func TestReadToolsAreAnnotatedReadOnly(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	res, err := e.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	readTools := map[string]bool{"list_folders": true, "list_messages": true}
	for _, tool := range res.Tools {
		if readTools[tool.Name] && (tool.Annotations == nil || !tool.Annotations.ReadOnlyHint) {
			t.Errorf("%s is not annotated read-only", tool.Name)
		}
	}
}

// No tool sends mail. When send_draft arrives (milestone 0.4) it will be the
// only exception, and this list must be updated deliberately.
func TestNoSendTools(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	res, err := e.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		n := strings.ToLower(tool.Name)
		sends := strings.Contains(n, "send") || strings.Contains(n, "forward") ||
			(strings.Contains(n, "reply") && !strings.Contains(n, "draft"))
		if sends {
			t.Errorf("tool %q looks like it sends mail", tool.Name)
		}
	}
}

// Listing through the tools never marks a message as read.
func TestToolsDoNotMarkAsRead(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.imap.AddMessage(t, "INBOX", msgFromBob)
	e.imap.AddMessage(t, "INBOX", msgFromCarol)

	var folders ListFoldersOutput
	e.callOK(t, "list_folders", map[string]any{}, &folders)
	var msgs ListMessagesOutput
	e.callOK(t, "list_messages", map[string]any{"folder": "INBOX"}, &msgs)

	if n := e.imap.Unseen(t, "INBOX"); n != 2 {
		t.Fatalf("unseen = %d after listing, want 2", n)
	}
}

// The Bridge password never appears in tool results or logs.
func TestNoSecretsInOutputOrLogs(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.imap.AddMessage(t, "INBOX", msgFromBob)

	var all strings.Builder
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"list_folders", map[string]any{}},
		{"list_messages", map[string]any{"folder": "INBOX"}},
		{"list_messages", map[string]any{"folder": "Nope"}}, // error path
	} {
		res := e.call(t, c.tool, c.args)
		all.WriteString(text(res))
		raw, _ := json.Marshal(res.StructuredContent) // nil on error results
		all.Write(raw)
	}
	all.WriteString(e.logs.String())

	if strings.Contains(all.String(), testutil.Password) {
		t.Fatal("the Bridge password appears in tool output or logs")
	}
	if !strings.Contains(e.logs.String(), "connected to Bridge") {
		t.Error("logs were not captured; the check above proves nothing")
	}
}

// Message content stays out of the logs: only metadata is logged.
func TestNoMailContentInLogs(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.imap.AddMessage(t, "INBOX", msgFromBob)
	var out ListMessagesOutput
	e.callOK(t, "list_messages", map[string]any{"folder": "INBOX"}, &out)

	for _, s := range []string{"Lunch on Friday", "bob@example.org", "Are you free"} {
		if strings.Contains(e.logs.String(), s) {
			t.Errorf("logs contain mail content %q", s)
		}
	}
}
