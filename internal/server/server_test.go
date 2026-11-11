package server

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestServerHandshake connects an in-memory client and checks the server
// initializes and answers tools/list.
func TestServerHandshake(t *testing.T) {
	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()

	ss, err := New("test").Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() }) // closing an in-memory session cannot fail meaningfully

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() }) // closing an in-memory session cannot fail meaningfully

	if got := cs.InitializeResult().ServerInfo.Name; got != Name {
		t.Errorf("server name = %q, want %q", got, Name)
	}
	if _, err := cs.ListTools(ctx, nil); err != nil {
		t.Fatalf("list tools: %v", err)
	}
}
