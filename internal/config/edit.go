package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// MaxMaxChanges caps the changes-per-run option the owner may set.
const MaxMaxChanges = 10000

// ErrConfirmSend means sending was to be switched on without the owner
// confirming it (issue #181).
var ErrConfirmSend = errors.New("switching sending on needs your confirmation")

// OptionValues are the owner's options as the Settings page and
// `proton-mail-mcp settings set` change them. "" for a folder means its
// default.
type OptionValues struct {
	AllowRemoteImages bool
	AttachmentDir     string
	OutboxDir         string
	MaxChanges        int
	AllowSend         bool
}

// OptionValues returns the options s holds, with the defaults where it
// holds none.
func (s Settings) OptionValues() OptionValues {
	v := OptionValues{AttachmentDir: s.AttachmentDir, OutboxDir: s.OutboxDir, MaxChanges: DefaultMaxChanges}
	if s.AllowRemoteImages != nil {
		v.AllowRemoteImages = *s.AllowRemoteImages
	}
	if s.MaxChanges > 0 {
		v.MaxChanges = s.MaxChanges
	}
	if s.AllowSend != nil {
		v.AllowSend = *s.AllowSend
	}
	return v
}

// ApplyOptions checks v and returns saved with it written in, changing
// nothing else: never the user, the certificate or the password (which
// isn't in it). An option whose variable is in locked (set in .env or the
// environment instead) keeps its saved value. Switching sending on when it
// was off needs confirmSend. The Settings page and `settings set` both save
// through here, so they check the same way.
func ApplyOptions(saved Settings, v OptionValues, locked map[string]bool, confirmSend bool) (Settings, error) {
	v.AttachmentDir = strings.TrimSpace(v.AttachmentDir)
	v.OutboxDir = strings.TrimSpace(v.OutboxDir)
	switch {
	case v.MaxChanges < 1 || v.MaxChanges > MaxMaxChanges:
		return Settings{}, fmt.Errorf("changes per run must be a whole number from 1 to %d", MaxMaxChanges)
	case v.AttachmentDir != "" && !filepath.IsAbs(v.AttachmentDir):
		return Settings{}, errors.New("the attachment folder must be a full path, like D:\\Mail attachments")
	case v.OutboxDir != "" && !filepath.IsAbs(v.OutboxDir):
		return Settings{}, errors.New("the outbox folder must be a full path, like D:\\Mail to send")
	}
	if saved.User == "" {
		return Settings{}, errors.New("set up Proton Mail MCP first: the options are saved with its settings")
	}
	if !locked[EnvAllowRemoteImages] {
		on := v.AllowRemoteImages
		saved.AllowRemoteImages = &on
	}
	if !locked[EnvAttachmentDir] {
		saved.AttachmentDir = ""
		if v.AttachmentDir != "" {
			saved.AttachmentDir = filepath.Clean(v.AttachmentDir)
		}
	}
	if !locked[EnvOutboxDir] {
		saved.OutboxDir = ""
		if v.OutboxDir != "" {
			saved.OutboxDir = filepath.Clean(v.OutboxDir)
		}
	}
	if !locked[EnvAllowSend] {
		wasOn := saved.AllowSend != nil && *saved.AllowSend
		if v.AllowSend && !wasOn && !confirmSend {
			return Settings{}, ErrConfirmSend
		}
		on := v.AllowSend
		saved.AllowSend = &on
	}
	if !locked[EnvMaxChanges] {
		saved.MaxChanges = 0
		if v.MaxChanges != DefaultMaxChanges {
			saved.MaxChanges = v.MaxChanges
		}
	}
	return saved, nil
}

// Locked returns the option variables that .env or the environment set,
// which override config.json.
func Locked(dotenv map[string]string, saved Settings) map[string]bool {
	out := map[string]bool{}
	for _, key := range []string{EnvAllowRemoteImages, EnvAttachmentDir, EnvOutboxDir, EnvMaxChanges, EnvAllowSend} {
		switch Origin(key, dotenv, saved) {
		case FromEnvironment, FromDotEnv:
			out[key] = true
		}
	}
	return out
}
