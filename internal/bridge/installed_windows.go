//go:build windows

package bridge

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Installed reports whether Proton Mail Bridge is installed, and where. On
// Windows it is an MSI with DisplayName "Proton Mail Bridge" and Publisher
// "Proton AG" under the machine's Uninstall key; the product code may change
// between versions, so it is matched by name
// (docs/installation-research.md §A1).
func Installed() (string, bool) {
	for _, root := range []string{
		`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`,
		`SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`,
	} {
		if loc, ok := findUninstall(root); ok {
			return loc, true
		}
	}
	return "", false
}

func findUninstall(root string) (string, bool) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, root, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return "", false
	}
	defer func() { _ = k.Close() }() // read-only key
	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return "", false
	}
	for _, name := range names {
		sub, err := registry.OpenKey(k, name, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		display, _, _ := sub.GetStringValue("DisplayName")
		publisher, _, _ := sub.GetStringValue("Publisher")
		location, _, _ := sub.GetStringValue("InstallLocation")
		_ = sub.Close() // read-only key
		if strings.EqualFold(display, "Proton Mail Bridge") && strings.EqualFold(publisher, "Proton AG") {
			if location == "" {
				location = display
			}
			return location, true
		}
	}
	return "", false
}
