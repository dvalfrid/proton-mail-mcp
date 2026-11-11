// Command proton-mail-mcp is an MCP server (stdio) that exposes a Proton Mail
// inbox to Claude via Proton Mail Bridge on localhost.
//
// Settings come from the environment, or from a .env file: the one named by
// PROTON_MCP_ENV_FILE, else .env next to the executable. The environment wins
// over the file.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvalfrid/proton-mail-mcp/internal/config"
	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
	"github.com/dvalfrid/proton-mail-mcp/internal/server"
)

// version is set at build time: -ldflags "-X main.version=...".
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		_, _ = fmt.Fprintln(os.Stderr, "proton-mail-mcp", version) // stdout is reserved for MCP
		return
	}

	// stdout carries the MCP protocol; all logging goes to stderr.
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	envFile, err := envFilePath()
	if err != nil {
		return err
	}
	dotenv, err := config.ReadDotEnv(envFile)
	if err != nil {
		return err
	}
	cfg, err := config.Load(config.Lookup(dotenv))
	if err != nil {
		return fmt.Errorf("%w (settings file: %s)", err, envFile)
	}
	logger.Info("starting", "version", version, "config", cfg)

	tlsCfg, err := mail.TLSConfig(cfg.Host, cfg.CertFile, logger)
	if err != nil {
		return err
	}
	mailClient := mail.New(mail.Options{
		Addr: cfg.IMAPAddr(), User: cfg.User, Password: cfg.Password,
		TLSConfig: tlsCfg, Logger: logger,
	})
	defer func() { _ = mailClient.Close() }() // best effort on shutdown

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	err = server.New(version, mailClient).Run(ctx, &mcp.StdioTransport{})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func envFilePath() (string, error) {
	if p := os.Getenv("PROTON_MCP_ENV_FILE"); p != "" {
		return p, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate executable: %w", err)
	}
	return filepath.Join(filepath.Dir(exe), ".env"), nil
}
