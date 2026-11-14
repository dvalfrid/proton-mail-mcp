package wizard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dvalfrid/proton-mail-mcp/internal/config"
)

// MaxMaxChanges caps the changes-per-run option the page accepts.
const MaxMaxChanges = 10000

// Settings is the Settings page (issue #158): the owner's options as they
// are saved, and which ones .env or the environment set instead. Never the
// password or anything about the account.
type Settings struct {
	SetUp                bool              `json:"setUp"` // setup has been run: there is a settings file to save into
	AllowRemoteImages    bool              `json:"allowRemoteImages"`
	AttachmentDir        string            `json:"attachmentDir"` // "" for the default
	DefaultAttachmentDir string            `json:"defaultAttachmentDir"`
	MaxChanges           int               `json:"maxChanges"`
	DefaultMaxChanges    int               `json:"defaultMaxChanges"`
	Locked               map[string]string `json:"locked"` // option → where it is set instead: "environment" or ".env"
}

// The options by the name the page uses, with their variables.
var optionVars = map[string]string{
	"allowRemoteImages": config.EnvAllowRemoteImages,
	"attachmentDir":     config.EnvAttachmentDir,
	"maxChanges":        config.EnvMaxChanges,
}

// SettingsOnly makes the window open on the Settings page instead of the
// setup steps (proton-mail-mcp-setup -settings).
func (b *Backend) SettingsOnly() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.settingsOnly = true
}

// SetPickFolder sets the native folder picker, once the window exists.
func (b *Backend) SetPickFolder(pick func(start string) (string, error)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pickFolder = pick
}

// dotEnv reads the .env file the server reads: PROTON_MCP_ENV_FILE, or the
// one next to it.
func (b *Backend) dotEnv() map[string]string {
	path := os.Getenv("PROTON_MCP_ENV_FILE")
	if path == "" {
		path = filepath.Join(filepath.Dir(b.o.Exe), ".env")
	}
	m, err := config.ReadDotEnv(path)
	if err != nil {
		return map[string]string{}
	}
	return m
}

func (b *Backend) getSettings() Settings {
	saved, err := config.ReadSettings(config.SettingsFile(b.o.Dir))
	s := Settings{SetUp: err == nil && saved.User != "", DefaultMaxChanges: config.DefaultMaxChanges, MaxChanges: config.DefaultMaxChanges, Locked: map[string]string{}}
	s.DefaultAttachmentDir, _ = config.DefaultAttachmentDir()
	if saved.AllowRemoteImages != nil {
		s.AllowRemoteImages = *saved.AllowRemoteImages
	}
	s.AttachmentDir = saved.AttachmentDir
	if saved.MaxChanges > 0 {
		s.MaxChanges = saved.MaxChanges
	}
	dotenv := b.dotEnv()
	for name, key := range optionVars {
		switch o := config.Origin(key, dotenv, saved); o {
		case config.FromEnvironment, config.FromDotEnv:
			s.Locked[name] = o
		}
	}
	return s
}

// saveSettings writes the options the page sent into the settings file,
// changing nothing else in it: never the user, the certificate or the
// password (which isn't in it). A locked option keeps its saved value.
func (b *Backend) saveSettings(args json.RawMessage) (Settings, error) {
	var a struct {
		AllowRemoteImages bool
		AttachmentDir     string
		MaxChanges        int
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return Settings{}, errors.New("bad settings request")
	}
	a.AttachmentDir = strings.TrimSpace(a.AttachmentDir)
	switch {
	case a.MaxChanges < 1 || a.MaxChanges > MaxMaxChanges:
		return Settings{}, fmt.Errorf("changes per run must be a whole number from 1 to %d", MaxMaxChanges)
	case a.AttachmentDir != "" && !filepath.IsAbs(a.AttachmentDir):
		return Settings{}, errors.New("the attachment folder must be a full path, like D:\\Mail attachments")
	}
	file := config.SettingsFile(b.o.Dir)
	saved, err := config.ReadSettings(file)
	if err != nil {
		return Settings{}, fmt.Errorf("the settings could not be read (%w); run setup again first", err)
	}
	if saved.User == "" {
		return Settings{}, errors.New("set up Proton Mail MCP first: the options are saved with its settings")
	}
	locked := b.getSettings().Locked
	if locked["allowRemoteImages"] == "" {
		on := a.AllowRemoteImages
		saved.AllowRemoteImages = &on
	}
	if locked["attachmentDir"] == "" {
		saved.AttachmentDir = ""
		if a.AttachmentDir != "" {
			saved.AttachmentDir = filepath.Clean(a.AttachmentDir)
		}
	}
	if locked["maxChanges"] == "" {
		saved.MaxChanges = 0
		if a.MaxChanges != config.DefaultMaxChanges {
			saved.MaxChanges = a.MaxChanges
		}
	}
	if err := config.WriteSettings(file, saved); err != nil {
		return Settings{}, err
	}
	return b.getSettings(), nil
}

// pickAttachmentDir opens the native folder picker, starting at the
// current folder. "" if the owner cancelled.
func (b *Backend) pickAttachmentDir(start string) (string, error) {
	if b.pickFolder == nil {
		return "", errors.New("no folder picker here: type the full path instead")
	}
	return b.pickFolder(start)
}
