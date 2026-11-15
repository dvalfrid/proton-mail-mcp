package config

import "golang.org/x/sys/windows"

// downloadsDir is the Downloads known folder, which follows the owner if
// they moved it (to another drive or OneDrive).
func downloadsDir() (string, error) {
	return windows.KnownFolderPath(windows.FOLDERID_Downloads, 0)
}

// documentsDir is the Documents known folder, which follows the owner if
// they moved it.
func documentsDir() (string, error) {
	return windows.KnownFolderPath(windows.FOLDERID_Documents, 0)
}
