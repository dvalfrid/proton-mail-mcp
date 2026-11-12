package wizard

import (
	_ "embed"
	"errors"
	"os"
	"path/filepath"
)

// Page is the wizard's single HTML page, with its styles and script inline.
// Its Content-Security-Policy allows nothing from outside the page.
//
//go:embed page.html
var Page string

// ErrNoWebView means no web view is available to show the wizard in; the
// terminal setup still works.
var ErrNoWebView = errors.New("no web view available (WebView2 on Windows)")

// DataPath is the web view's profile folder, under the user's local app
// data (WebView2 would otherwise put it in the roaming profile).
func DataPath() (string, error) {
	dir, err := os.UserCacheDir() // %LOCALAPPDATA% on Windows
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "proton-mail-mcp", "WebView2"), nil
}
