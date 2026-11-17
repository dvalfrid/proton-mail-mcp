package check

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dvalfrid/proton-mail-mcp/internal/config"
	"github.com/dvalfrid/proton-mail-mcp/internal/keystore"
)

// EnvFile is the .env file the server at exe reads: the one named by
// PROTON_MCP_ENV_FILE, else .env next to the server.
func EnvFile(exe string) string {
	if p := os.Getenv("PROTON_MCP_ENV_FILE"); p != "" {
		return p
	}
	return filepath.Join(filepath.Dir(exe), ".env")
}

// Load reads the settings as the server does: the environment over the .env
// file over the settings from setup (in dir), with the password from get
// (the OS credential store) when it is in neither. The second result names
// where settings were looked for. The server, `proton-mail-mcp check` and the
// setup window's Status page all load through here, so they can't disagree.
func Load(envFile, dir string, get func(user string) (string, error)) (config.Config, string, error) {
	where := envFile + " or " + config.SettingsFile(dir)
	dotenv, err := config.ReadDotEnv(envFile)
	if err != nil {
		return config.Config{}, where, err
	}
	saved, err := config.ReadSettings(config.SettingsFile(dir))
	if err != nil {
		return config.Config{}, where, err
	}
	files := config.Chain(config.Lookup(dotenv), config.FromMap(saved.Values()))
	cfg, err := loadWithStore(files, get)
	if err != nil {
		return config.Config{}, where, fmt.Errorf("%w (%s, or see %s)", err, setUpAgain, where)
	}
	cfg.Options = config.Options(cfg, dotenv, saved)
	return cfg, where, nil
}

// loadWithStore loads the settings, with the password from the credential
// store when it is in neither the environment nor .env. A store that fails
// (locked, no D-Bus) is reported as such, not as a missing password
// (issue #74).
func loadWithStore(files func(string) (string, bool), get func(string) (string, error)) (config.Config, error) {
	var storeErr error
	cfg, err := config.Load(config.Chain(files, config.PasswordFrom(files, func(user string) (string, error) {
		pw, err := get(user)
		if err != nil && !errors.Is(err, keystore.ErrNotFound) {
			storeErr = err
		}
		return pw, err
	})))
	if err != nil && storeErr != nil {
		return config.Config{}, fmt.Errorf("the OS credential store could not be read (%w); unlock it, or %s: %w", storeErr, setUpAgain, err)
	}
	return cfg, err
}

// Diagnose loads the settings as the server does and runs every step: what
// `proton-mail-mcp check` prints and the Status page shows.
func Diagnose(ctx context.Context, envFile, dir string, get func(string) (string, error)) []Step {
	cfg, where, err := Load(envFile, dir, get)
	return Run(ctx, Input{Config: cfg, LoadErr: err, SettingsFile: where})
}
