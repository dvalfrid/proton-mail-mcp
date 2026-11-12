//go:build windows

package bridge

import "testing"

func TestInsideDir(t *testing.T) {
	t.Parallel()
	dir := `C:\Program Files`
	for path, want := range map[string]bool{
		`C:\Program Files\Proton AG\Proton Mail Bridge\bridge.exe`: true,
		`c:\program files\proton ag\bridge.exe`:                    true,
		`C:\Program Files Evil\bridge.exe`:                         false,
		`C:\Users\someone\AppData\Local\Temp\bridge.exe`:           false,
		`C:\Program Files\..\Users\someone\bridge.exe`:             false,
		`C:\Program Files`: false,
	} {
		if got := insideDir(path, dir); got != want {
			t.Errorf("insideDir(%q) = %v, want %v", path, got, want)
		}
	}
}

// The real Program Files folder, from Windows, holds this machine's own
// programs; a temp folder does not.
func TestUnderProgramFiles(t *testing.T) {
	t.Parallel()
	if underProgramFiles(t.TempDir() + `\bridge.exe`) {
		t.Error("a temp folder counted as Program Files")
	}
}
