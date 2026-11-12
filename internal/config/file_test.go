package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSettingsRoundTrip(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "proton-mail-mcp")
	path := SettingsFile(dir)

	if s, err := ReadSettings(path); err != nil || s != (Settings{}) {
		t.Fatalf("missing file: %+v, %v", s, err)
	}
	want := Settings{User: "alice@example.com", Host: "127.0.0.1", IMAPPort: 1144, SMTPPort: 1026, CertFile: CertPath(dir), Fingerprint: "AA:BB"}
	if err := WriteSettings(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSettings(path)
	if err != nil || got != want {
		t.Fatalf("read back %+v, %v; want %+v", got, err, want)
	}
	if runtime.GOOS != "windows" { // Windows has no Unix permission bits
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("mode %v, want 0600", info.Mode().Perm())
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Errorf("dir holds %v (err %v): want only config.json, no temporary file", entries, err)
	}
}

// The environment wins over .env, .env over the settings file, and the
// password comes from the credential store only when it is in neither.
func TestChainPrecedence(t *testing.T) {
	t.Parallel()
	env := FromMap(map[string]string{EnvIMAPPort: "2000"})
	dotenv := FromMap(map[string]string{EnvIMAPPort: "3000", EnvSMTPPort: "3001"})
	file := FromMap(Settings{User: "alice@example.com", IMAPPort: 4000, SMTPPort: 4001, CertFile: "c.pem", Host: "127.0.0.1"}.Values())
	files := Chain(env, dotenv, file)
	store := func(user string) (string, error) {
		if user == "alice@example.com" {
			return "from-store", nil
		}
		return "", errors.New("not found")
	}
	cfg, err := Load(Chain(files, PasswordFrom(files, store)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IMAPPort != 2000 || cfg.SMTPPort != 3001 || cfg.CertFile != "c.pem" || cfg.Password.Reveal() != "from-store" {
		t.Errorf("cfg = %+v (password %q)", cfg, cfg.Password.Reveal())
	}

	withPw := Chain(FromMap(map[string]string{EnvPassword: "from-env"}), file)
	cfg, err = Load(Chain(withPw, PasswordFrom(withPw, store)))
	if err != nil || cfg.Password.Reveal() != "from-env" {
		t.Errorf("env password not preferred: %q, %v", cfg.Password.Reveal(), err)
	}
}
