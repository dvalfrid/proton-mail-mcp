package wizard

import (
	"errors"
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
	if err := f.call(t, "state", nil, &st); err != nil || st.Mode != "app" || st.Start != "settings" {
		t.Fatalf("mode %q, start %q, %v", st.Mode, st.Start, err)
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

	dir, outbox := filepath.Join(t.TempDir(), "Mail attachments"), filepath.Join(t.TempDir(), "Mail to send")
	if err := f.call(t, "saveSettings", map[string]any{"allowRemoteImages": true, "attachmentDir": dir, "outboxDir": outbox, "maxChanges": 30}, &s); err != nil {
		t.Fatal(err)
	}
	got, err := config.ReadSettings(file)
	if err != nil || got.AllowRemoteImages == nil || !*got.AllowRemoteImages || got.AttachmentDir != dir || got.OutboxDir != outbox || got.MaxChanges != 30 ||
		got.User != setUp.User || got.CertFile != setUp.CertFile || got.Fingerprint != setUp.Fingerprint || got.IMAPPort != setUp.IMAPPort {
		t.Errorf("saved %+v, %v", got, err)
	}

	for name, args := range map[string]map[string]any{
		"zero changes":     {"maxChanges": 0},
		"too many changes": {"maxChanges": MaxMaxChanges + 1},
		"relative folder":  {"maxChanges": 5, "attachmentDir": "attachments"},
		"relative outbox":  {"maxChanges": 5, "outboxDir": "outbox"},
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
	if got, _ := config.ReadSettings(file); got.AttachmentDir != "" || got.OutboxDir != "" || got.MaxChanges != 0 || got.AllowRemoteImages == nil || *got.AllowRemoteImages {
		t.Errorf("defaults saved as %+v", got)
	}
}

// An option set in .env is shown as locked, and saving keeps its saved value.
func TestSettingsLockedByDotEnv(t *testing.T) {
	t.Parallel()
	f := newFixture(t, true)
	exeDir := t.TempDir()
	f.b.o.Exe = filepath.Join(exeDir, "proton-mail-mcp.exe")
	if err := os.WriteFile(filepath.Join(exeDir, ".env"), []byte(config.EnvMaxChanges+"=7\n"+config.EnvAllowRemoteImages+"=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := config.SettingsFile(f.o.Dir)
	off := false
	if err := config.WriteSettings(file, config.Settings{User: "alice@example.com", MaxChanges: 40, AllowRemoteImages: &off}); err != nil {
		t.Fatal(err)
	}
	var s Settings
	if err := f.call(t, "getSettings", nil, &s); err != nil || s.Locked["maxChanges"] != ".env" || s.Locked["allowRemoteImages"] != ".env" || s.Locked["attachmentDir"] != "" {
		t.Fatalf("locked %v, %v", s.Locked, err)
	}
	// The page shows the value in effect, from .env, not the saved one.
	if s.MaxChanges != 7 || !s.AllowRemoteImages {
		t.Errorf("locked options show %d, %v; want .env's 7, on", s.MaxChanges, s.AllowRemoteImages)
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

// Sending is switched on only with the page's confirmation, and off
// without one (issue #181).
func TestSettingsSendNeedsConfirmation(t *testing.T) {
	t.Parallel()
	f := newFixture(t, true)
	file := config.SettingsFile(f.o.Dir)
	if err := config.WriteSettings(file, config.Settings{User: "alice@example.com", CertFile: "c.pem"}); err != nil {
		t.Fatal(err)
	}
	if err := f.call(t, "saveSettings", map[string]any{"maxChanges": 200, "allowSend": true}, nil); !errors.Is(err, errConfirmSend) {
		t.Errorf("on without confirmation: %v", err)
	}
	if got, _ := config.ReadSettings(file); got.AllowSend != nil {
		t.Errorf("saved %+v", got)
	}
	var s Settings
	if err := f.call(t, "saveSettings", map[string]any{"maxChanges": 200, "allowSend": true, "confirmSend": true}, &s); err != nil || !s.AllowSend {
		t.Fatalf("on with confirmation: %+v, %v", s, err)
	}
	// Already on: saving other options needs no new confirmation.
	if err := f.call(t, "saveSettings", map[string]any{"maxChanges": 50, "allowSend": true}, nil); err != nil {
		t.Errorf("save while on: %v", err)
	}
	if err := f.call(t, "saveSettings", map[string]any{"maxChanges": 50}, &s); err != nil || s.AllowSend {
		t.Errorf("off: %+v, %v", s, err)
	}
	if got, _ := config.ReadSettings(file); got.AllowSend == nil || *got.AllowSend {
		t.Errorf("off saved as %+v", got)
	}
}

// The Settings page reads and saves label colors, refuses a color not in
// the palette, and shows those .env sets as locked (issue #209).
func TestSettingsLabelColors(t *testing.T) {
	t.Parallel()
	f := newFixture(t, true)
	exeDir := t.TempDir()
	f.b.o.Exe = filepath.Join(exeDir, "proton-mail-mcp.exe")
	file := config.SettingsFile(f.o.Dir)
	if err := config.WriteSettings(file, config.Settings{User: "alice@example.com"}); err != nil {
		t.Fatal(err)
	}
	var s Settings
	if err := f.call(t, "getSettings", nil, &s); err != nil || len(s.LabelColors) != 0 || len(s.Colors) != len(config.Colors) {
		t.Fatalf("before: %+v, %v", s, err)
	}
	args := map[string]any{"maxChanges": 5, "labelColors": map[string]string{"Work": "blue", "Family": "green"}}
	if err := f.call(t, "saveSettings", args, &s); err != nil || s.LabelColors["Work"] != "blue" || s.LabelColors["Family"] != "green" {
		t.Fatalf("saved %v, %v", s.LabelColors, err)
	}
	if got, _ := config.ReadSettings(file); len(got.LabelColors) != 2 {
		t.Errorf("in the file: %v", got.LabelColors)
	}
	if err := f.call(t, "saveSettings", map[string]any{"maxChanges": 5, "labelColors": map[string]string{"Work": "teal"}}, nil); err == nil {
		t.Error("teal was saved")
	}

	if err := os.WriteFile(filepath.Join(exeDir, ".env"), []byte(config.EnvLabelColors+`={"Travel":"red"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var locked Settings // a fresh one: decoding into s would merge the maps
	if err := f.call(t, "getSettings", nil, &locked); err != nil || locked.Locked["labelColors"] != ".env" || len(locked.LabelColors) != 1 || locked.LabelColors["Travel"] != "red" {
		t.Errorf("locked by .env: %v %v, %v", locked.Locked, locked.LabelColors, err)
	}
	if err := f.call(t, "saveSettings", map[string]any{"maxChanges": 5, "labelColors": map[string]string{}}, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := config.ReadSettings(file); len(got.LabelColors) != 2 {
		t.Errorf("a locked option was changed: %v", got.LabelColors)
	}
}
