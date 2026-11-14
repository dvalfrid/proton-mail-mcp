package wizard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dvalfrid/proton-mail-mcp/internal/config"
)

// The Settings page reads and saves the owner's options, and nothing else:
// the user, certificate and fingerprint stay as setup wrote them, and bad
// values are refused without writing (issue #158).
func TestSettingsPage(t *testing.T) {
	t.Parallel()
	f := newFixture(t, true)
	f.b.SettingsOnly()
	var st State
	if err := f.call(t, "state", nil, &st); err != nil || st.Mode != "settings" {
		t.Fatalf("mode %q, %v", st.Mode, err)
	}

	// Before setup there is nothing to save into.
	var s Settings
	if err := f.call(t, "getSettings", nil, &s); err != nil || s.SetUp {
		t.Fatalf("before setup: %+v, %v", s, err)
	}
	if err := f.call(t, "saveSettings", map[string]any{"maxChanges": 5}, nil); err == nil || !strings.Contains(err.Error(), "set up") {
		t.Errorf("save before setup: %v", err)
	}
	f.nothingWritten(t)

	file := config.SettingsFile(f.o.Dir)
	setUp := config.Settings{User: "alice@example.com", Host: "127.0.0.1", IMAPPort: 1143, CertFile: "c.pem", Fingerprint: "AA"}
	if err := config.WriteSettings(file, setUp); err != nil {
		t.Fatal(err)
	}
	if err := f.call(t, "getSettings", nil, &s); err != nil || !s.SetUp || s.AllowRemoteImages || s.MaxChanges != config.DefaultMaxChanges {
		t.Fatalf("defaults: %+v, %v", s, err)
	}

	dir := filepath.Join(t.TempDir(), "Mail attachments")
	if err := f.call(t, "saveSettings", map[string]any{"allowRemoteImages": true, "attachmentDir": dir, "maxChanges": 30}, &s); err != nil {
		t.Fatal(err)
	}
	got, err := config.ReadSettings(file)
	if err != nil || got.AllowRemoteImages == nil || !*got.AllowRemoteImages || got.AttachmentDir != dir || got.MaxChanges != 30 ||
		got.User != setUp.User || got.CertFile != setUp.CertFile || got.Fingerprint != setUp.Fingerprint || got.IMAPPort != setUp.IMAPPort {
		t.Errorf("saved %+v, %v", got, err)
	}

	for name, args := range map[string]map[string]any{
		"zero changes":     {"maxChanges": 0},
		"too many changes": {"maxChanges": MaxMaxChanges + 1},
		"relative folder":  {"maxChanges": 5, "attachmentDir": "attachments"},
	} {
		if err := f.call(t, "saveSettings", args, nil); err == nil {
			t.Errorf("%s: saved", name)
		}
	}
	if after, _ := config.ReadSettings(file); after.MaxChanges != 30 {
		t.Errorf("a refused save changed the file: %+v", after)
	}

	// Back to the defaults: nothing is kept but off for images.
	if err := f.call(t, "saveSettings", map[string]any{"maxChanges": config.DefaultMaxChanges}, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := config.ReadSettings(file); got.AttachmentDir != "" || got.MaxChanges != 0 || got.AllowRemoteImages == nil || *got.AllowRemoteImages {
		t.Errorf("defaults saved as %+v", got)
	}
}

// An option set in .env is shown as locked, and saving keeps its saved value.
func TestSettingsLockedByDotEnv(t *testing.T) {
	t.Parallel()
	f := newFixture(t, true)
	exeDir := t.TempDir()
	f.b.o.Exe = filepath.Join(exeDir, "proton-mail-mcp.exe")
	if err := os.WriteFile(filepath.Join(exeDir, ".env"), []byte(config.EnvMaxChanges+"=7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := config.SettingsFile(f.o.Dir)
	if err := config.WriteSettings(file, config.Settings{User: "alice@example.com", MaxChanges: 40}); err != nil {
		t.Fatal(err)
	}
	var s Settings
	if err := f.call(t, "getSettings", nil, &s); err != nil || s.Locked["maxChanges"] != ".env" || s.Locked["allowRemoteImages"] != "" {
		t.Fatalf("locked %v, %v", s.Locked, err)
	}
	if err := f.call(t, "saveSettings", map[string]any{"maxChanges": 99}, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := config.ReadSettings(file); got.MaxChanges != 40 {
		t.Errorf("a locked option was changed: %d", got.MaxChanges)
	}
}

// Without a window there is no folder picker: the page says to type.
func TestPickFolderWithoutAWindow(t *testing.T) {
	t.Parallel()
	f := newFixture(t, true)
	if err := f.call(t, "pickFolder", map[string]any{"start": ""}, nil); err == nil || !strings.Contains(err.Error(), "type the full path") {
		t.Errorf("pickFolder: %v", err)
	}
}
