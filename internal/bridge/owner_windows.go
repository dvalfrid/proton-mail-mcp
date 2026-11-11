//go:build windows

package bridge

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// PortOwner reports which program listens on 127.0.0.1:port, and whether
// its Authenticode signature is valid. It is advice for the user, never a
// gate: an impostor could be signed too, and pinning the certificate is
// what actually protects the password (docs/installation-research.md §C).
func PortOwner(port int) (Owner, error) {
	pid, err := listenerPID(port)
	if err != nil {
		return Owner{}, err
	}
	path, err := processPath(pid)
	if err != nil {
		return Owner{PID: pid}, err
	}
	return Owner{PID: pid, Path: path, Signed: signatureValid(path)}, nil
}

var procGetExtendedTCPTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

const (
	afINET                   = 2
	tcpTableOwnerPIDListener = 3  // TCP_TABLE_OWNER_PID_LISTENER
	tcpRowSize               = 24 // MIB_TCPROW_OWNER_PID: six DWORDs
)

// listenerPID finds the process listening on 127.0.0.1:port (or on any
// address) in the IPv4 TCP table.
func listenerPID(port int) (uint32, error) {
	var size uint32
	// The first call reports the size needed.
	_, _, _ = procGetExtendedTCPTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, afINET, tcpTableOwnerPIDListener, 0) //nolint:gosec // G103: Win32 call; only size is used
	if size == 0 {
		return 0, errors.New("GetExtendedTcpTable returned no size")
	}
	buf := make([]byte, size)
	r, _, _ := procGetExtendedTCPTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, afINET, tcpTableOwnerPIDListener, 0) //nolint:gosec // G103: Win32 call into a buffer of the size it asked for
	if r != 0 {
		return 0, fmt.Errorf("GetExtendedTcpTable: error %d", r)
	}
	n := binary.LittleEndian.Uint32(buf[0:4])
	for i := range n {
		row := buf[4+int(i)*tcpRowSize:]
		if len(row) < tcpRowSize {
			break
		}
		addr := row[4:8] // dwLocalAddr, network order
		// dwLocalPort: the port in network byte order in the low 16 bits.
		p := int(binary.BigEndian.Uint16(row[8:10]))
		loopbackOrAny := (addr[0] == 127 && addr[1] == 0 && addr[2] == 0 && addr[3] == 1) || binary.LittleEndian.Uint32(addr) == 0
		if p == port && loopbackOrAny {
			return binary.LittleEndian.Uint32(row[20:24]), nil
		}
	}
	return 0, fmt.Errorf("no process listens on port %d", port)
}

func processPath(pid uint32) (string, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", fmt.Errorf("open process %d: %w", pid, err)
	}
	defer func() { _ = windows.CloseHandle(h) }() // query-only handle
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf)) //nolint:gosec // G115: MAX_LONG_PATH fits in uint32
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", fmt.Errorf("process %d path: %w", pid, err)
	}
	return windows.UTF16ToString(buf[:size]), nil
}

// signatureValid asks WinVerifyTrust whether the file carries a valid
// Authenticode signature. No UI, no revocation check (offline is fine).
func signatureValid(path string) bool {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	file := windows.WinTrustFileInfo{Size: uint32(unsafe.Sizeof(windows.WinTrustFileInfo{})), FilePath: p}
	data := windows.WinTrustData{
		Size:                            uint32(unsafe.Sizeof(windows.WinTrustData{})),
		UIChoice:                        windows.WTD_UI_NONE,
		RevocationChecks:                windows.WTD_REVOKE_NONE,
		UnionChoice:                     windows.WTD_CHOICE_FILE,
		StateAction:                     windows.WTD_STATEACTION_VERIFY,
		FileOrCatalogOrBlobOrSgnrOrCert: unsafe.Pointer(&file), //nolint:gosec // G103: the Win32 struct takes a pointer to the file info
	}
	err = windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, &data)
	data.StateAction = windows.WTD_STATEACTION_CLOSE
	_ = windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, &data) // releases the state
	return err == nil
}
