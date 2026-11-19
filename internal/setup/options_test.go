package setup

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dvalfrid/proton-mail-mcp/internal/config"
)

func settingsCmd(t *testing.T, answer string) (SettingsCommand, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	saved := config.Settings{User: "alice@example.com", Host: "127.0.0.1", IMAPPort: 1143, CertFile: "c.pem", Fingerprint: "AA"}
	if err := config.WriteSettings(config.SettingsFile(dir), saved); err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	return SettingsCommand{Dir: dir, EnvFile: filepath.Join(dir, ".env"), In: strings.NewReader(answer), Out: out}, out
}

func saved(t *testing.T, c SettingsCommand) config.Settings {
	t.Helper()
	s, err := config.ReadSettings(config.SettingsFile(c.Dir))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSettingsCommand(t *testing.T) {
	t.Parallel()
	c, out := settingsCmd(t, "")
	if err := c.Run(nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"allow_send", "off  (default)", "max_changes          200"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("show lacks %q:\n%s", want, out)
		}
	}

	dir := filepath.Join(t.TempDir(), "Mail attachments")
	for _, args := range [][]string{
		{"set", "allow_remote_images", "on"},
		{"set", "max_changes", "30"},
		{"set", "attachment_dir", dir},
	} {
		if err := c.Run(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	s := saved(t, c)
	if s.AllowRemoteImages == nil || !*s.AllowRemoteImages || s.MaxChanges != 30 || s.AttachmentDir != dir ||
		s.User != "alice@example.com" || s.CertFile != "c.pem" || s.Fingerprint != "AA" || s.IMAPPort != 1143 {
		t.Errorf("saved %+v", s)
	}

	for _, args := range [][]string{
		{"set", "max_changes", "0"},
		{"set", "max_changes", "many"},
		{"set", "attachment_dir", "relative"},
		{"set", "allow_remote_images", "maybe"},
		{"set", "user", "mallory@example.com"},
		{"set", "password", "x"},
		{"unset", "allow_send"},
	} {
		if err := c.Run(args); err == nil {
			t.Errorf("%v: accepted", args)
		}
	}
	if s2 := saved(t, c); s2.MaxChanges != 30 || s2.User != "alice@example.com" {
		t.Errorf("a refused set changed the file: %+v", s2)
	}

	if err := c.Run([]string{"set", "attachment_dir", "default"}); err != nil || saved(t, c).AttachmentDir != "" {
		t.Errorf("back to default: %v", err)
	}
}

// Sending is switched on only after the owner types yes.
func TestSettingsCommandSendNeedsYes(t *testing.T) {
	t.Parallel()
	for answer, on := range map[string]bool{"yes\n": true, "YES\r\n": true, "no\n": false, "": false, "y\n": false} {
		c, out := settingsCmd(t, answer)
		err := c.Run([]string{"set", "allow_send", "on"})
		s := saved(t, c)
		if got := s.AllowSend != nil && *s.AllowSend; got != on || (err == nil) != on {
			t.Errorf("answer %q: on=%v, err=%v", answer, got, err)
		}
		if !strings.Contains(out.String(), "Type yes") {
			t.Errorf("answer %q: not asked:\n%s", answer, out)
		}
	}
	// Off needs no confirmation.
	c, _ := settingsCmd(t, "")
	if err := c.Run([]string{"set", "allow_send", "off"}); err != nil {
		t.Error(err)
	}
}

// An option set in .env can't be changed here; the command says where.
func TestSettingsCommandLocked(t *testing.T) {
	t.Parallel()
	c, out := settingsCmd(t, "yes\n")
	if err := os.WriteFile(c.EnvFile, []byte(config.EnvAllowSend+"=0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := c.Run([]string{"set", "allow_send", "on"})
	if err == nil || !strings.Contains(err.Error(), ".env") {
		t.Errorf("locked: %v", err)
	}
	if s := saved(t, c); s.AllowSend != nil {
		t.Errorf("saved %+v", s)
	}
	if err := c.Run(nil); err != nil || !strings.Contains(out.String(), ".env, which overrides") {
		t.Errorf("show: %v\n%s", err, out)
	}
	if !errors.Is(c.Run([]string{"set"}), errUsage) {
		t.Error("no usage error")
	}
}

// label_color gives one label a color, or takes it away, keeping the
// others (issue #209).
func TestSettingsCommandLabelColor(t *testing.T) {
	t.Parallel()
	c, out := settingsCmd(t, "")
	for _, args := range [][]string{
		{"set", "label_color", "Work", "blue"},
		{"set", "label_color", "Family", "Green"},
		{"set", "label_color", "work", "red"}, // the same label, whatever its case
	} {
		if err := c.Run(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	if got := saved(t, c).LabelColors; len(got) != 2 || got["work"] != "red" || got["Family"] != "green" {
		t.Errorf("saved %v", got)
	}
	out.Reset()
	if err := c.Run(nil); err != nil || !strings.Contains(out.String(), "Family=green, work=red") {
		t.Errorf("show: %v\n%s", err, out)
	}
	for _, args := range [][]string{
		{"set", "label_color", "Work", "pink"},
		{"set", "label_color", "Work"},
		{"set", "label_color", "", "blue"},
	} {
		if err := c.Run(args); err == nil {
			t.Errorf("%v: accepted", args)
		}
	}
	if err := c.Run([]string{"set", "label_color", "WORK", "none"}); err != nil {
		t.Fatal(err)
	}
	if got := saved(t, c).LabelColors; len(got) != 1 || got["Family"] != "green" {
		t.Errorf("after none: %v", got)
	}
}

// update_channel switches release candidates on and off (issue #213).
func TestSettingsCommandUpdateChannel(t *testing.T) {
	t.Parallel()
	c, out := settingsCmd(t, "")
	if err := c.Run(nil); err != nil || !strings.Contains(out.String(), "update_channel       stable  (default)") {
		t.Errorf("show: %v\n%s", err, out)
	}
	if err := c.Run([]string{"set", "update_channel", "test"}); err != nil || saved(t, c).UpdateChannel != config.ChannelTest {
		t.Errorf("test: %v, %+v", err, saved(t, c))
	}
	if err := c.Run([]string{"set", "update_channel", "beta"}); err == nil {
		t.Error("beta accepted")
	}
	if err := c.Run([]string{"set", "update_channel", "stable"}); err != nil || saved(t, c).UpdateChannel != "" {
		t.Errorf("stable: %v, %+v", err, saved(t, c))
	}
}
