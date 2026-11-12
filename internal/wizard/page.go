package wizard

import (
	_ "embed"
	"errors"
)

// Page is the wizard's single HTML page, with its styles and script inline.
// Its Content-Security-Policy allows nothing from outside the page.
//
//go:embed page.html
var Page string

// ErrNoWebView means no web view is available to show the wizard in; the
// terminal setup still works.
var ErrNoWebView = errors.New("no web view available (WebView2 on Windows)")
