// Command proton-mail-mcp is an MCP server (stdio) that exposes a Proton Mail
// inbox to Claude via Proton Mail Bridge on localhost.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvalfrid/proton-mail-mcp/internal/server"
)

// version is set at build time: -ldflags "-X main.version=..."
var version = "dev"

func main() {
	// stdout carries the MCP protocol; all logging goes to stderr.
	log.SetOutput(os.Stderr)
	log.SetPrefix("proton-mail-mcp: ")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := server.New(version).Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
