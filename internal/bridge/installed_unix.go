//go:build !windows

package bridge

import (
	"os"
	"path/filepath"
	"runtime"
)

// Installed reports whether Proton Mail Bridge is installed, and where, by
// looking at the places its packages put it (docs/installation-research.md
// §A1): the app bundle on macOS; on Linux the deb/rpm/Arch layout, the
// Flatpak export, and the launcher on PATH-like locations.
func Installed() (string, bool) {
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{"/Applications/Proton Mail Bridge.app"}
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, "Applications", "Proton Mail Bridge.app"))
		}
	default:
		candidates = []string{
			"/usr/lib/protonmail/bridge/bridge",
			"/usr/bin/protonmail-bridge",
			"/usr/bin/protonmail-bridge-core",
			"/var/lib/flatpak/exports/bin/ch.protonmail.protonmail-bridge",
		}
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, ".local", "share", "flatpak", "exports", "bin", "ch.protonmail.protonmail-bridge"))
		}
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	return "", false
}
