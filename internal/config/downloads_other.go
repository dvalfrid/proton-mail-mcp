//go:build !windows

package config

import (
	"os"
	"path/filepath"
)

// downloadsDir is ~/Downloads, where browsers save on macOS and most Linux
// desktops.
func downloadsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Downloads"), nil
}
