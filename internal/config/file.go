package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Settings is what `proton-mail-mcp setup` writes to the user's config
// directory. It never holds the password: that is in the OS credential
// store. The pinned certificate is a PEM file next to it.
type Settings struct {
	User        string `json:"user"`
	Host        string `json:"host"`
	IMAPPort    int    `json:"imap_port"`
	SMTPPort    int    `json:"smtp_port"`
	CertFile    string `json:"cert_file"`
	Fingerprint string `json:"cert_sha256"` // shown by `check`; the file is what is trusted
}

// EnvConfigDir overrides Dir, for tests and portable installs.
const EnvConfigDir = "PROTON_MCP_CONFIG_DIR"

// Dir is where setup keeps its files: $PROTON_MCP_CONFIG_DIR if set, else
// <user config dir>/proton-mail-mcp.
func Dir() (string, error) {
	if d := os.Getenv(EnvConfigDir); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find the user config directory: %w", err)
	}
	return filepath.Join(base, "proton-mail-mcp"), nil
}

// SettingsFile is the settings path inside dir.
func SettingsFile(dir string) string { return filepath.Join(dir, "config.json") }

// CertPath is the pinned certificate's path inside dir.
func CertPath(dir string) string { return filepath.Join(dir, "bridge-cert.pem") }

// ReadSettings reads a settings file. A missing file gives zero Settings and
// no error.
func ReadSettings(path string) (Settings, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: our own settings file in the user's config dir
	if errors.Is(err, os.ErrNotExist) {
		return Settings{}, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("read %s: %w", path, err)
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return Settings{}, fmt.Errorf("read %s: %w", path, err)
	}
	return s, nil
}

// WriteFile writes data to path atomically (a temporary file, then a
// rename), readable only by the user, creating the directory (also
// user-only) if needed.
func WriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op after the rename
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// WriteSettings saves s as JSON with WriteFile.
func WriteSettings(path string, s Settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(path, append(data, '\n'))
}

// Values returns the settings as environment-variable values, for Chain.
// Empty fields are left out.
func (s Settings) Values() map[string]string {
	m := map[string]string{}
	set := func(k, v string) {
		if v != "" {
			m[k] = v
		}
	}
	set(EnvUser, s.User)
	set(EnvHost, s.Host)
	set(EnvCert, s.CertFile)
	if s.IMAPPort != 0 {
		m[EnvIMAPPort] = strconv.Itoa(s.IMAPPort)
	}
	if s.SMTPPort != 0 {
		m[EnvSMTPPort] = strconv.Itoa(s.SMTPPort)
	}
	return m
}

// Chain returns a lookup that asks each lookup in turn; the first that has
// the key wins.
func Chain(lookups ...func(string) (string, bool)) func(string) (string, bool) {
	return func(key string) (string, bool) {
		for _, l := range lookups {
			if v, ok := l(key); ok {
				return v, true
			}
		}
		return "", false
	}
}

// FromMap is a lookup over a map.
func FromMap(m map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := m[key]
		return v, ok
	}
}

// PasswordFrom is a lookup that answers only EnvPassword, by asking get for
// the password of the user that lookup gives. It lets the password come
// from the credential store when it is not in the environment or .env.
func PasswordFrom(lookup func(string) (string, bool), get func(user string) (string, error)) func(string) (string, bool) {
	return func(key string) (string, bool) {
		if key != EnvPassword {
			return "", false
		}
		user, ok := lookup(EnvUser)
		if !ok || user == "" {
			return "", false
		}
		pw, err := get(user)
		if err != nil || pw == "" {
			return "", false
		}
		return pw, true
	}
}
