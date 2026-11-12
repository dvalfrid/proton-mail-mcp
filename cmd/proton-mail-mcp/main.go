// Command proton-mail-mcp is an MCP server (stdio) that exposes a Proton Mail
// inbox to Claude via Proton Mail Bridge on localhost.
//
// Settings come from, in order of precedence: the environment; a .env file
// (the one named by PROTON_MCP_ENV_FILE, else .env next to the executable);
// and the settings that proton-mail-mcp setup wrote to the user's config
// directory, with the password in the OS credential store.
//
// Commands: (none) runs the MCP server on stdio; setup sets it up
// interactively; check diagnoses the connection to Bridge.
package main

import (
	"context"
	"crypto/tls"
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
	"github.com/dvalfrid/proton-mail-mcp/internal/help"
	"github.com/dvalfrid/proton-mail-mcp/internal/keystore"
	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
	"github.com/dvalfrid/proton-mail-mcp/internal/server"
	"github.com/dvalfrid/proton-mail-mcp/internal/setup"
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

	switch flag.Arg(0) {
	case "":
	case "check":
		os.Exit(runCheck())
	case "setup":
		os.Exit(runSetup(flag.Args()[1:]))
	default:
		logger.Error("unknown command", "command", flag.Arg(0), "usage", "proton-mail-mcp [-version] [setup | check]")
		os.Exit(2)
	}

	if err := run(logger); err != nil {
		logger.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

// loadConfig reads the settings: the environment over the .env file over
// the settings from setup, with the password from the credential store if
// it is in neither. The second result names where settings were looked for.
func loadConfig() (config.Config, string, error) {
	envFile, err := envFilePath()
	if err != nil {
		return config.Config{}, "", err
	}
	dir, err := config.Dir()
	if err != nil {
		return config.Config{}, envFile, err
	}
	where := envFile + " or " + config.SettingsFile(dir)
	dotenv, err := config.ReadDotEnv(envFile)
	if err != nil {
		return config.Config{}, where, err
	}
	saved, err := config.ReadSettings(config.SettingsFile(dir))
	if err != nil {
		return config.Config{}, where, err
	}
	files := config.Chain(config.Lookup(dotenv), config.FromMap(saved.Values()))
	cfg, err := config.Load(config.Chain(files, config.PasswordFrom(files, keystore.OS{}.Get)))
	if err != nil {
		return config.Config{}, where, fmt.Errorf("%w (run `proton-mail-mcp setup`, or see %s)", err, where)
	}
	return cfg, where, nil
}

// runSetup runs the interactive setup in the terminal and returns the exit
// code. Output goes to stderr: stdout is reserved for MCP, even here.
func runSetup(args []string) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	imapPort := fs.Int("imap-port", 0, "Bridge's IMAP port, if not 1143")
	smtpPort := fs.Int("smtp-port", 0, "Bridge's SMTP port, if not 1025")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	dir, err := config.Dir()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err) // nowhere else to report it
		return 1
	}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "locate this program:", err) // nowhere else to report it
		return 1
	}
	err = setup.Run(context.Background(), setup.Options{
		Out: os.Stderr, Prompt: &setup.Terminal{In: os.Stdin, Out: os.Stderr}, Store: keystore.OS{},
		Dir: dir, Exe: exe, IMAPPort: *imapPort, SMTPPort: *smtpPort, Clients: setup.DefaultClients(),
	})
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "setup did not finish:", err) // nowhere else to report it
		return 1
	}
	return runCheck()
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
	mailClient := connect(logger)
	defer func() { _ = mailClient.Close() }() // best effort on shutdown

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	err := server.New(version, mailClient).Run(ctx, &mcp.StdioTransport{})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// connect returns the mail client for the settings. Without usable
// settings it returns one whose every call says to run setup: the server
// still starts, so Claude can tell the owner in the chat, rather than the
// client only showing a disconnected server. No connection is made then.
func connect(logger *slog.Logger) *mail.Client {
	cfg, _, err := loadConfig()
	if err == nil {
		var tlsCfg *tls.Config
		if tlsCfg, err = mail.TLSConfig(cfg.Host, cfg.CertFile); err == nil {
			// Only non-secret fields are logged; the Config itself is never
			// passed to the logger.
			logger.Info("starting", "version", version, "imap", cfg.IMAPAddr(), "allow_send", cfg.AllowSend)
			// A clear warning in the client's log if Bridge isn't usable
			// right now. The server starts anyway: Bridge may start later.
			if err := check.Startup(context.Background(), cfg); err != nil {
				logger.Warn("Bridge not ready", "err", err, "help", help.URL(help.BridgeDown))
			}
			return mail.New(mail.Options{
				Addr: cfg.IMAPAddr(), User: cfg.User, Password: cfg.Password,
				TLSConfig: tlsCfg, Logger: logger,
			})
		}
	}
	logger.Error("not set up; every tool will say so", "version", version, "err", err, "help", help.URL(help.NotSetUp))
	return mail.NewUnconfigured(err, logger)
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
