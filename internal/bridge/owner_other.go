//go:build !windows

package bridge

import "errors"

// ErrOwnerUnsupported means this OS has no port-owner check. On macOS it
// would need lsof and codesign, and Linux packages carry no signature to
// check (docs/installation-research.md §C); pinning the certificate is the
// real protection everywhere.
var ErrOwnerUnsupported = errors.New("checking which program owns the port is only done on Windows")

// PortOwner is only implemented on Windows.
func PortOwner(int) (Owner, error) { return Owner{}, ErrOwnerUnsupported }
