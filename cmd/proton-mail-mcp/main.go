// Command proton-mail-mcp is an MCP server (stdio) that exposes a Proton Mail
// inbox to Claude via Proton Mail Bridge on localhost.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvalfrid/proton-mail-mcp/internal/server"
)

// version is set at build time: -ldflags "-X main.version=...".
var version = "dev"

func main() {
	// stdout carries the MCP protocol; all logging goes to stderr.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	if err := run(); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	slog.Info("starting", "version", version)
	return server.New(version).Run(ctx, &mcp.StdioTransport{})
}
