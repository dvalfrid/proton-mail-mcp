package config

import (
	"fmt"
	"path/filepath"
)

// attachmentFolderName is the folder under Downloads that attachments are
// saved into by default.
const attachmentFolderName = "Proton Mail MCP"

// DefaultAttachmentDir is the folder EnvAttachmentDir defaults to: "Proton
// Mail MCP" in the owner's Downloads folder.
func DefaultAttachmentDir() (string, error) {
	d, err := downloadsDir()
	if err != nil {
		return "", fmt.Errorf("find the Downloads folder: %w", err)
	}
	return filepath.Join(d, attachmentFolderName), nil
}
