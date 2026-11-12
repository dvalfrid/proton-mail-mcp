package bridge

import (
	"path/filepath"
	"testing"
)

// A signature alone doesn't make a program Bridge: any signed program can be
// copied somewhere and renamed bridge.exe (issue #71).
func TestTrustedBridge(t *testing.T) {
	t.Parallel()
	exe := filepath.Join("Proton AG", "Proton Mail Bridge", "bridge.exe")
	tests := []struct {
		name string
		o    Owner
		want bool
	}{
		{"signed, in Program Files", Owner{Path: exe, Signed: true, ProtectedDir: true}, true},
		{"signed, elsewhere", Owner{Path: exe, Signed: true}, false},
		{"unsigned, in Program Files", Owner{Path: exe, ProtectedDir: true}, false},
		{"another name", Owner{Path: filepath.Join("x", "node.exe"), Signed: true, ProtectedDir: true}, false},
	}
	for _, tt := range tests {
		if got := tt.o.TrustedBridge(); got != tt.want {
			t.Errorf("%s: TrustedBridge = %v, want %v", tt.name, got, tt.want)
		}
	}
}
