package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testPassword = "s3cret-test-password"

func lookupMap(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func validEnv() map[string]string {
	return map[string]string{EnvUser: "alice@example.com", EnvPassword: testPassword, EnvCert: "bridge.pem"}
}

func TestLoadDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := Load(lookupMap(validEnv()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.IMAPAddr() != "127.0.0.1:1143" || cfg.SMTPAddr() != "127.0.0.1:1025" {
		t.Errorf("addrs = %s, %s", cfg.IMAPAddr(), cfg.SMTPAddr())
	}
	if cfg.AllowSend {
		t.Error("AllowSend must default to false")
	}
	if cfg.Password.Reveal() != testPassword {
		t.Error("password not loaded")
	}
}

func TestLoadValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		set     map[string]string
		wantErr string
	}{
		{"missing user", map[string]string{EnvUser: ""}, EnvUser},
		{"missing password", map[string]string{EnvPassword: ""}, EnvPassword},
		{"missing certificate", map[string]string{EnvCert: ""}, EnvCert},
		{"remote host", map[string]string{EnvHost: "192.168.1.10"}, "only 127.0.0.1"},
		{"remote name", map[string]string{EnvHost: "bridge.example.com"}, "only 127.0.0.1"},
		{"bad port", map[string]string{EnvIMAPPort: "99999"}, "not a port"},
		{"bad send flag", map[string]string{EnvAllowSend: "yes"}, EnvAllowSend},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := validEnv()
			for k, v := range tt.set {
				env[k] = v
			}
			_, err := Load(lookupMap(env))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.wantErr)
			}
			if strings.Contains(err.Error(), testPassword) {
				t.Fatal("error message contains the password")
			}
		})
	}
}

func TestLocalHosts(t *testing.T) {
	t.Parallel()
	for _, h := range []string{"127.0.0.1", "::1", "localhost", "LOCALHOST", "127.0.0.2"} {
		if !IsLocalHost(h) {
			t.Errorf("IsLocalHost(%q) = false", h)
		}
	}
	for _, h := range []string{"0.0.0.0", "10.0.0.1", "example.com", "", "localhost.example.com"} {
		if IsLocalHost(h) {
			t.Errorf("IsLocalHost(%q) = true", h)
		}
	}
}

func TestAllowSend(t *testing.T) {
	t.Parallel()
	for v, want := range map[string]bool{"1": true, "true": true, "0": false, "false": false, "": false} {
		env := validEnv()
		env[EnvAllowSend] = v
		cfg, err := Load(lookupMap(env))
		if err != nil {
			t.Fatalf("%q: %v", v, err)
		}
		if cfg.AllowSend != want {
			t.Errorf("%s=%q: AllowSend = %v", EnvAllowSend, v, cfg.AllowSend)
		}
	}
}

func TestMaxChanges(t *testing.T) {
	t.Parallel()
	for v, want := range map[string]int{"": DefaultMaxChanges, "1": 1, "1000": 1000} {
		env := validEnv()
		env[EnvMaxChanges] = v
		cfg, err := Load(lookupMap(env))
		if err != nil || cfg.MaxChanges != want {
			t.Errorf("%s=%q: MaxChanges = %d, %v; want %d", EnvMaxChanges, v, cfg.MaxChanges, err, want)
		}
	}
	for _, v := range []string{"0", "-5", "many"} {
		env := validEnv()
		env[EnvMaxChanges] = v
		if _, err := Load(lookupMap(env)); err == nil || !strings.Contains(err.Error(), EnvMaxChanges) {
			t.Errorf("%s=%q: err = %v, want a refusal", EnvMaxChanges, v, err)
		}
	}
}

// Guardrail: the password never appears when a Config is printed, logged or
// marshalled.
func TestSecretNeverPrinted(t *testing.T) {
	t.Parallel()
	cfg, err := Load(lookupMap(validEnv()))
	if err != nil {
		t.Fatal(err)
	}

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	logger.Info("config", "cfg", cfg, "pw", cfg.Password)
	slog.New(slog.NewTextHandler(&logBuf, nil)).Info("config", "cfg", cfg, "pw", cfg.Password)

	js, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}

	outputs := map[string]string{
		"%v":   fmt.Sprintf("%v", cfg),
		"%+v":  fmt.Sprintf("%+v", cfg),
		"%#v":  fmt.Sprintf("%#v", cfg),
		"%s":   fmt.Sprintf("%s|%v", cfg.Password, cfg.Password),
		"json": string(js),
		"slog": logBuf.String(),
	}
	for name, out := range outputs {
		if strings.Contains(out, testPassword) {
			t.Errorf("%s output contains the password: %s", name, out)
		}
	}
}

func TestReadDotEnv(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "# comment\n\nPROTON_BRIDGE_USER=alice@example.com\nexport PROTON_BRIDGE_PASSWORD=\"quoted value\"\nPROTON_BRIDGE_CERT='C:/certs/bridge.pem'\nEMPTY=\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadDotEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		EnvUser:     "alice@example.com",
		EnvPassword: "quoted value",
		EnvCert:     "C:/certs/bridge.pem",
		"EMPTY":     "",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestReadDotEnvMissingAndInvalid(t *testing.T) {
	t.Parallel()
	got, err := ReadDotEnv(filepath.Join(t.TempDir(), "nope.env"))
	if err != nil || len(got) != 0 {
		t.Fatalf("missing file: got %v, %v", got, err)
	}

	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("PROTON_BRIDGE_PASSWORD "+testPassword+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = ReadDotEnv(path)
	if err == nil {
		t.Fatal("want error for a line without '='")
	}
	if strings.Contains(err.Error(), testPassword) {
		t.Fatal("parse error echoes the line, which can hold the password")
	}
}
