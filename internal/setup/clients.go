package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/dvalfrid/proton-mail-mcp/internal/config"
)

// ServerName is the name the server is registered under in MCP clients.
const ServerName = "proton-mail"

// ClaudeCode registers the server through Claude Code's own command, `claude
// mcp add --scope user`, rather than editing ~/.claude.json, which holds
// much other state.
type ClaudeCode struct{}

// Name implements Client.
func (ClaudeCode) Name() string { return "Claude Code" }

// Available implements Client: the claude command is on PATH.
func (ClaudeCode) Available() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}

// Registered implements Client.
func (ClaudeCode) Registered() bool {
	return claude("mcp", "get", ServerName) == nil
}

// Register implements Client.
func (ClaudeCode) Register(exe string) error {
	return claude("mcp", "add", "--scope", "user", ServerName, "--", exe)
}

func claude(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "claude", args...).CombinedOutput() //nolint:gosec // G204: fixed command; the only variable is this program's own path
	if err != nil {
		return fmt.Errorf("claude %s: %w: %s", args[1], err, clip(string(out)))
	}
	return nil
}

func clip(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// ClaudeDesktop registers the server in one claude_desktop_config.json. The
// file is merged, never replaced: other servers and settings stay as they
// are, a backup is kept, and the write is atomic. No password goes in it.
type ClaudeDesktop struct {
	Path  string // the config file
	Label string // e.g. "Claude Desktop" or "Claude Desktop (Microsoft Store)"
}

// Name implements Client.
func (d ClaudeDesktop) Name() string { return d.Label }

// Available implements Client: the app's config directory exists.
func (d ClaudeDesktop) Available() bool {
	info, err := os.Stat(filepath.Dir(d.Path))
	return err == nil && info.IsDir()
}

// Registered implements Client.
func (d ClaudeDesktop) Registered() bool {
	cfg, err := d.read()
	if err != nil {
		return false
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	_, ok := servers[ServerName]
	return ok
}

// Register implements Client.
func (d ClaudeDesktop) Register(exe string) error {
	cfg, err := d.read()
	if err != nil {
		return err
	}
	if data, err := os.ReadFile(d.Path); err == nil {
		if err := config.WriteFile(d.Path+".bak", data); err != nil {
			return fmt.Errorf("back up %s: %w", d.Path, err)
		}
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[ServerName] = map[string]any{"command": exe}
	cfg["mcpServers"] = servers
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFile(d.Path, append(data, '\n'))
}

// read returns the config as generic JSON, so unknown fields survive. A
// missing file is an empty config; an unreadable one is an error, so it is
// never overwritten.
func (d ClaudeDesktop) read() (map[string]any, error) {
	data, err := os.ReadFile(d.Path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", d.Path, err)
	}
	cfg := map[string]any{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("%s is not valid JSON, so it is left alone: %w", d.Path, err)
		}
	}
	return cfg, nil
}

// DefaultClients are the clients on this machine: Claude Code, and Claude
// Desktop's config, including the Microsoft Store build's own location on
// Windows (docs/installation-research.md §E).
func DefaultClients() []Client {
	clients := []Client{ClaudeCode{}}
	if dir, err := os.UserConfigDir(); err == nil {
		clients = append(clients, ClaudeDesktop{Path: filepath.Join(dir, "Claude", "claude_desktop_config.json"), Label: "Claude Desktop"})
	}
	if runtime.GOOS == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			clients = append(clients, ClaudeDesktop{
				Path:  filepath.Join(local, "Packages", "Claude_pzs8sxrjxfjjc", "LocalCache", "Roaming", "Claude", "claude_desktop_config.json"),
				Label: "Claude Desktop (Microsoft Store)",
			})
		}
	}
	return clients
}
