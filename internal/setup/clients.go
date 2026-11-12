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
	"strings"
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

// Registration implements Client: the Command line of `claude mcp get`.
func (ClaudeCode) Registration() (string, bool) {
	out, err := claudeOutput("mcp", "get", ServerName)
	if err != nil {
		return "", false
	}
	return commandLine(out), true
}

// commandLine is the program in `claude mcp get` output ("Command: ...").
func commandLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if cmd, ok := strings.CutPrefix(strings.TrimSpace(line), "Command:"); ok {
			return strings.TrimSpace(cmd)
		}
	}
	return ""
}

// Register implements Client. An earlier registration (of another copy) is
// replaced: `claude mcp add` refuses a name that exists.
func (c ClaudeCode) Register(exe string) error {
	if _, ok := c.Registration(); ok {
		if err := claude("mcp", "remove", "--scope", "user", ServerName); err != nil {
			return err
		}
	}
	return claude("mcp", "add", "--scope", "user", ServerName, "--", exe)
}

// Unregister implements Client: only a registration that starts exe is
// removed.
func (c ClaudeCode) Unregister(exe string) (bool, error) {
	if cmd, ok := c.Registration(); !ok || !SameExe(cmd, exe) {
		return false, nil
	}
	return true, claude("mcp", "remove", "--scope", "user", ServerName)
}

func claude(args ...string) error {
	_, err := claudeOutput(args...)
	return err
}

func claudeOutput(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", args...) //nolint:gosec // G204: fixed command; the only variable is this program's own path
	noConsole(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("claude %s: %w: %s", args[1], err, clip(string(out)))
	}
	return string(out), nil
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

// Registration implements Client.
func (d ClaudeDesktop) Registration() (string, bool) {
	cfg, err := d.read()
	if err != nil {
		return "", false
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	entry, ok := servers[ServerName]
	if !ok {
		return "", false
	}
	m, _ := entry.(map[string]any)
	cmd, _ := m["command"].(string)
	return cmd, true
}

// SameExe reports whether two paths name the same program: compared
// cleaned, and on Windows without regard to case.
func SameExe(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
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

// Unregister implements Client. The file is merged as in Register, with a
// backup; only an entry that starts exe is removed.
func (d ClaudeDesktop) Unregister(exe string) (bool, error) {
	cfg, err := d.read()
	if err != nil {
		return false, err
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	entry, _ := servers[ServerName].(map[string]any)
	command, _ := entry["command"].(string)
	if !SameExe(command, exe) {
		return false, nil
	}
	if data, err := os.ReadFile(d.Path); err == nil {
		if err := config.WriteFile(d.Path+".bak", data); err != nil {
			return false, fmt.Errorf("back up %s: %w", d.Path, err)
		}
	}
	delete(servers, ServerName)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false, err
	}
	return true, config.WriteFile(d.Path, append(data, '\n'))
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

// DefaultClients are the clients on this machine: Claude Code, and every
// place Claude Desktop may keep its config (config.ClaudeDesktopConfigs).
func DefaultClients() []Client {
	clients := []Client{ClaudeCode{}}
	dir, _ := os.UserConfigDir() // "" leaves out the standard location
	for _, d := range config.ClaudeDesktopConfigs(dir, os.Getenv("LOCALAPPDATA")) {
		clients = append(clients, ClaudeDesktop{Path: d.Path, Label: d.Label})
	}
	return clients
}
