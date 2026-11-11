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

	"github.com/dvalfrid/proton-mail-mcp/internal/check"
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

	if flag.Arg(0) == "check" {
		os.Exit(runCheck())
	}
	if flag.NArg() > 0 {
		logger.Error("unknown command", "command", flag.Arg(0), "usage", "proton-mail-mcp [-version] [check]")
		os.Exit(2)
	}

	if err := run(logger); err != nil {
		logger.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

// loadConfig reads the settings: the environment, over the .env file.
func loadConfig() (config.Config, string, error) {
	envFile, err := envFilePath()
	if err != nil {
		return config.Config{}, "", err
	}
	dotenv, err := config.ReadDotEnv(envFile)
	if err != nil {
		return config.Config{}, envFile, err
	}
	cfg, err := config.Load(config.Lookup(dotenv))
	if err != nil {
		return config.Config{}, envFile, fmt.Errorf("%w (settings file: %s)", err, envFile)
	}
	return cfg, envFile, nil
}

// runCheck prints the diagnostics to stderr (stdout is reserved for MCP,
// even here) and returns the exit code.
func runCheck() int {
	cfg, envFile, err := loadConfig()
	steps := check.Run(context.Background(), check.Input{Config: cfg, LoadErr: err, SettingsFile: envFile})
	check.Print(os.Stderr, steps)
	if check.Failed(steps) {
		return 1
	}
	return 0
}

func run(logger *slog.Logger) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}
	// Only non-secret fields are logged; the Config itself is never passed to
	// the logger.
	logger.Info("starting", "version", version, "imap", cfg.IMAPAddr(), "allow_send", cfg.AllowSend)

	tlsCfg, err := mail.TLSConfig(cfg.Host, cfg.CertFile)
	if err != nil {
		return err
	}
	// A clear warning in the client's log if Bridge isn't usable right now.
	// The server starts anyway: Bridge may be started later.
	if err := check.Startup(context.Background(), cfg); err != nil {
		logger.Warn("Bridge not ready", "err", err)
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
