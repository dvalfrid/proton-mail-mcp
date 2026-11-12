package setup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dvalfrid/proton-mail-mcp/internal/config"
)

// Uninstalling removes the server only where it starts this exe, and keeps
// the settings unless asked.
func TestUninstallKeepsSettingsByDefault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, f := range []string{config.SettingsFile(dir), config.CertPath(dir)} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ours := &fakeClient{available: true, got: "/opt/proton-mail-mcp"}
	other := &fakeClient{available: true, got: "/elsewhere/proton-mail-mcp"}
	forgot := false
	err := Uninstall(UninstallOptions{Exe: "/opt/proton-mail-mcp", Clients: []Client{ours, other}, Dir: dir, Forget: func() error { forgot = true; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if ours.got != "" || other.got == "" {
		t.Errorf("ours = %q (want removed), other = %q (want kept)", ours.got, other.got)
	}
	if forgot {
		t.Error("the password was removed without -remove-settings")
	}
	if _, err := os.Stat(config.SettingsFile(dir)); err != nil {
		t.Errorf("settings removed: %v", err)
	}
}

// With RemoveSettings it removes setup's files and the password, and leaves
// anything else in the folder.
func TestUninstallRemovesSettings(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	other := filepath.Join(dir, "notes.txt")
	for _, f := range []string{config.SettingsFile(dir), config.CertPath(dir), other} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	webview := filepath.Join(t.TempDir(), "proton-mail-mcp", "WebView2")
	if err := os.MkdirAll(filepath.Join(webview, "EBWebView"), 0o700); err != nil {
		t.Fatal(err)
	}
	forgot := false
	err := Uninstall(UninstallOptions{Exe: "/opt/proton-mail-mcp", RemoveSettings: true, Dir: dir,
		Forget: func() error { forgot = true; return nil }, WebViewDir: webview})
	if err != nil {
		t.Fatal(err)
	}
	if !forgot {
		t.Error("the password was kept")
	}
	for _, f := range []string{config.SettingsFile(dir), config.CertPath(dir), webview, filepath.Dir(webview)} {
		if _, err := os.Stat(f); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s still exists: %v", f, err)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("a file setup didn't write was removed: %v", err)
	}
}

// Claude Desktop's entry is removed only when it starts this exe; the rest
// of the config stays, with a backup.
func TestClaudeDesktopUnregister(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "claude_desktop_config.json")
	write := func(command string) {
		t.Helper()
		cfg := map[string]any{"theme": "dark", "mcpServers": map[string]any{
			ServerName: map[string]any{"command": command}, "other": map[string]any{"command": "x"},
		}}
		data, _ := json.Marshal(cfg)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	d := ClaudeDesktop{Path: path, Label: "Claude Desktop"}

	write(filepath.Join("elsewhere", "proton-mail-mcp.exe"))
	if ok, err := d.Unregister(filepath.Join("opt", "proton-mail-mcp.exe")); ok || err != nil {
		t.Fatalf("removed another program's entry: %v, %v", ok, err)
	}

	write(filepath.Join("opt", "proton-mail-mcp.exe"))
	if ok, err := d.Unregister(filepath.Join("opt", "proton-mail-mcp.exe")); !ok || err != nil {
		t.Fatalf("Unregister = %v, %v", ok, err)
	}
	var cfg map[string]any
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if _, ok := servers[ServerName]; ok || servers["other"] == nil || cfg["theme"] != "dark" {
		t.Errorf("config after = %v", cfg)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Errorf("no backup: %v", err)
	}
}
