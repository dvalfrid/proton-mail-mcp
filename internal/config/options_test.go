package config

import (
	"path/filepath"
	"slices"
	"testing"
)

// The owner's options are read from config.json, and .env and the
// environment override them, in that order (issue #158).
func TestOptionsFromTheSettingsFile(t *testing.T) {
	on, dir := true, filepath.Join(t.TempDir(), "saved")
	saved := Settings{User: "alice@example.com", CertFile: "c.pem", AllowRemoteImages: &on, AttachmentDir: dir, MaxChanges: 30}
	base := map[string]string{EnvPassword: "secret-for-tests"}
	load := func(dotenv map[string]string) Config {
		t.Helper()
		cfg, err := Load(Chain(Lookup(dotenv), FromMap(saved.Values()), FromMap(base)))
		if err != nil {
			t.Fatal(err)
		}
		cfg.Options = Options(cfg, dotenv, saved)
		return cfg
	}

	cfg := load(map[string]string{})
	if !cfg.AllowRemoteImages || cfg.AttachmentDir != dir || cfg.MaxChanges != 30 {
		t.Errorf("from config.json: %+v", cfg)
	}
	if got := origins(cfg); !slices.Equal(got, []string{FromSettings, FromSettings, FromSettings}) {
		t.Errorf("origins %q", got)
	}

	cfg = load(map[string]string{EnvMaxChanges: "5"})
	if cfg.MaxChanges != 5 || origins(cfg)[2] != FromDotEnv {
		t.Errorf(".env over config.json: %d from %q", cfg.MaxChanges, origins(cfg)[2])
	}

	t.Setenv(EnvAllowRemoteImages, "0")
	cfg = load(map[string]string{EnvAllowRemoteImages: "1"})
	if cfg.AllowRemoteImages || origins(cfg)[0] != FromEnvironment {
		t.Errorf("environment over .env: %v from %q", cfg.AllowRemoteImages, origins(cfg)[0])
	}

	saved = Settings{User: "alice@example.com", CertFile: "c.pem"}
	cfg = load(map[string]string{})
	if cfg.MaxChanges != DefaultMaxChanges || origins(cfg)[2] != FromDefault {
		t.Errorf("default: %d from %q", cfg.MaxChanges, origins(cfg)[2])
	}
}

func origins(cfg Config) []string {
	out := make([]string, 0, len(cfg.Options))
	for _, o := range cfg.Options {
		out = append(out, o.Origin)
	}
	return out
}

// Running setup again keeps the owner's options; an option that is off is
// written as off, not left out.
func TestKeepOptions(t *testing.T) {
	t.Parallel()
	off := false
	prev := Settings{User: "old@example.com", AllowRemoteImages: &off, AttachmentDir: "/a", MaxChanges: 9}
	s := Settings{User: "new@example.com"}.KeepOptions(prev)
	if s.User != "new@example.com" || s.AllowRemoteImages != &off || s.AttachmentDir != "/a" || s.MaxChanges != 9 {
		t.Errorf("%+v", s)
	}
	if v := s.Values()[EnvAllowRemoteImages]; v != "0" {
		t.Errorf("off is %q", v)
	}
}
