package setup

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Only the installer's renamed copies are removed: never the programs
// themselves, the settings, or any other file in the folder (issue #176).
func TestRemoveOldCopies(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	keep := []string{"proton-mail-mcp.exe", "proton-mail-mcp-setup.exe", ".env", "README.md", "uninstall.exe",
		"proton-mail-mcp.exe.old", "proton-mail-mcp.exe.oldx", "other.exe.old0", "proton-mail-mcp.exe.old0.txt"}
	gone := []string{"proton-mail-mcp.exe.old0", "proton-mail-mcp.exe.old12", "proton-mail-mcp-setup.exe.old0"}
	for _, n := range append(slices.Clone(keep), gone...) {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "proton-mail-mcp.exe.old7"), 0o700); err != nil {
		t.Fatal(err)
	}
	keep = append(keep, "proton-mail-mcp.exe.old7") // a folder, not a copy

	left, err := RemoveOldCopies(dir)
	if left != 0 || err != nil {
		t.Fatalf("left %d, %v", left, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	slices.Sort(got)
	slices.Sort(keep)
	if !slices.Equal(got, keep) {
		t.Errorf("left %v, want %v", got, keep)
	}
}
