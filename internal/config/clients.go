package config

import (
	"path/filepath"
	"runtime"
)

// DesktopConfig is one place Claude Desktop may keep its
// claude_desktop_config.json.
type DesktopConfig struct {
	Path  string
	Label string // shown by setup, e.g. "Claude Desktop (Microsoft Store)"
}

// msixFamily is the package family name of Claude Desktop's Microsoft Store
// (MSIX) build.
const msixFamily = "Claude_pzs8sxrjxfjjc"

// ClaudeDesktopConfigs lists every place Claude Desktop may read its config
// from. setup registers in the ones whose directory exists and check looks in
// all of them: one list, so the two can't disagree.
//
// configDir is os.UserConfigDir(); localAppData is %LOCALAPPDATA%, used only
// on Windows. Which file a given Store build reads is not documented
// (docs/installation-research.md §E): 5.1231 reads the Claude-Data one, older
// builds the virtualised LocalCache one.
func ClaudeDesktopConfigs(configDir, localAppData string) []DesktopConfig {
	const name = "claude_desktop_config.json"
	var out []DesktopConfig
	if configDir != "" {
		out = append(out, DesktopConfig{Path: filepath.Join(configDir, "Claude", name), Label: "Claude Desktop"})
	}
	if runtime.GOOS == "windows" && localAppData != "" {
		out = append(out,
			DesktopConfig{Path: filepath.Join(localAppData, "Claude-Data", "claude_pzs8sxrjxfjjc", name), Label: "Claude Desktop (Microsoft Store)"},
			DesktopConfig{Path: filepath.Join(localAppData, "Packages", msixFamily, "LocalCache", "Roaming", "Claude", name), Label: "Claude Desktop (Microsoft Store, older builds)"},
		)
	}
	return out
}
