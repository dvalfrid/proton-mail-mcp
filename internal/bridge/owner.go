package bridge

import (
	"path/filepath"
	"strings"
)

// Owner is the program that listens on Bridge's port.
type Owner struct {
	PID    uint32
	Path   string // the executable, if it could be read
	Signed bool   // a valid Authenticode signature, from any signer (Windows only)
	// ProtectedDir: the executable is under Program Files, which only
	// administrators can write (Windows only).
	ProtectedDir bool
}

// TrustedBridge reports whether the owner is Proton Mail Bridge as far as
// can be told without asking the user: named bridge.exe, validly signed,
// and in a folder only administrators can write. A signature alone isn't
// enough: any signed program keeps its signature when copied somewhere and
// renamed bridge.exe (issue #71). The signer's name is not checked.
func (o Owner) TrustedBridge() bool {
	return strings.EqualFold(filepath.Base(o.Path), "bridge.exe") && o.Signed && o.ProtectedDir
}
