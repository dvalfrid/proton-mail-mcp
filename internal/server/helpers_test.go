package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvalfrid/proton-mail-mcp/internal/config"
	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
	"github.com/dvalfrid/proton-mail-mcp/internal/testutil"
)

// env is a full stack for a test: in-memory IMAP server ← mail.Client ←
// MCP server ← in-memory MCP client, as Claude would drive it.
type env struct {
	imap    *testutil.IMAPServer
	session *mcp.ClientSession
	logs    *syncBuffer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	srv := testutil.NewIMAPServer(t)
	logs := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	tlsCfg, err := mail.TLSConfig(srv.Host, srv.CertFile(t), logger)
	if err != nil {
		t.Fatal(err)
	}
	m := mail.New(mail.Options{
		Addr: srv.Addr(), User: testutil.User, Password: config.Secret(testutil.Password),
		TLSConfig: tlsCfg, Timeout: 5 * time.Second, Logger: logger,
	})
	t.Cleanup(func() { _ = m.Close() })

	serverT, clientT := mcp.NewInMemoryTransports()
	ss, err := New("test", m).Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() }) // in-memory: closing cannot fail meaningfully

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() }) // in-memory: closing cannot fail meaningfully

	return &env{imap: srv, session: cs, logs: logs}
}

// call calls a tool and returns the raw result.
func (e *env) call(t *testing.T, name string, args any) *mcp.CallToolResult {
	t.Helper()
	res, err := e.session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return res
}

// callOK calls a tool, fails the test on a tool error, and decodes the
// structured result into out.
func (e *env) callOK(t *testing.T, name string, args, out any) {
	t.Helper()
	res := e.call(t, name, args)
	if res.IsError {
		t.Fatalf("%s returned an error: %s", name, text(res))
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode %s result: %v\n%s", name, err, raw)
	}
}

// text joins a result's text content.
func text(res *mcp.CallToolResult) string {
	var b bytes.Buffer
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
