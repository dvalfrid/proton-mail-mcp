package config

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestClaudeDesktopConfigs(t *testing.T) {
	t.Parallel()
	cfgDir, local := "cfg", "local"
	got := map[string]bool{}
	for _, d := range ClaudeDesktopConfigs(cfgDir, local) {
		if d.Label == "" {
			t.Errorf("%s has no label", d.Path)
		}
		got[d.Path] = true
	}
	want := []string{filepath.Join(cfgDir, "Claude", "claude_desktop_config.json")}
	if runtime.GOOS == "windows" {
		want = append(want,
			// The Store build 5.1231 reads this one (issue #42).
			filepath.Join(local, "Claude-Data", "claude_pzs8sxrjxfjjc", "claude_desktop_config.json"),
			filepath.Join(local, "Packages", "Claude_pzs8sxrjxfjjc", "LocalCache", "Roaming", "Claude", "claude_desktop_config.json"),
		)
	}
	for _, p := range want {
		if !got[p] {
			t.Errorf("missing %s; got %v", p, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d paths, want %d: %v", len(got), len(want), got)
	}
}
