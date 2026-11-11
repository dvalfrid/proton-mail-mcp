package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvalfrid/proton-mail-mcp/internal/testutil"
)

// TestBinaryOverStdio builds the real binary and drives it the way an MCP
// client does: over stdin/stdout, configured by a .env file, against an
// in-memory IMAP server. It catches anything that breaks the stdio protocol
// (a stray write to stdout) and checks configuration loading end to end.
func TestBinaryOverStdio(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	srv := testutil.NewIMAPServer(t)
	srv.AddMessage(t, "INBOX", "From: bob@example.org\nSubject: Hello\nDate: Mon, 02 Nov 2026 10:00:00 +0000\n\nHi.\n")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	dir := t.TempDir()
	bin := filepath.Join(dir, "proton-mail-mcp")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.CommandContext(ctx, "go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	envFile := filepath.Join(dir, "test.env")
	env := strings.Join([]string{
		"PROTON_BRIDGE_USER=" + testutil.User,
		"PROTON_BRIDGE_PASSWORD=" + testutil.Password,
		"PROTON_BRIDGE_IMAP_PORT=" + strconv.Itoa(srv.Port),
		"PROTON_BRIDGE_CERT=" + srv.CertFile(t),
	}, "\n")
	if err := os.WriteFile(envFile, []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = append(cleanEnv(), "PROTON_MCP_ENV_FILE="+envFile)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect: %v\nstderr:\n%s", err, stderr.String())
	}
	defer func() { _ = cs.Close() }() // stops the process

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_messages", Arguments: map[string]any{"folder": "INBOX"}})
	if err != nil {
		t.Fatalf("call: %v\nstderr:\n%s", err, stderr.String())
	}
	if res.IsError {
		t.Fatalf("tool error: %+v\nstderr:\n%s", res.Content, stderr.String())
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(tc.Text, "Hello") {
		t.Fatalf("result = %+v", res.Content)
	}
	if strings.Contains(stderr.String(), testutil.Password) {
		t.Fatal("the password appears in the server's log output")
	}
}

// cleanEnv is the test's environment without PROTON_* variables, so a
// developer's own settings cannot leak into (or satisfy) the test.
func cleanEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "PROTON_") {
			out = append(out, kv)
		}
	}
	return out
}
