//go:build !windows

package wizard

import "errors"

// Run is not available here yet (issue #91): ErrNoWebView, so the caller
// falls back to the terminal setup.
func Run(*Backend, string) error { return ErrNoWebView }

// SystemAccent is empty: the page's default.
func SystemAccent() Accent { return Accent{} }

// OpenURL is not available here yet.
func OpenURL(string) error { return errors.ErrUnsupported }

// Alert is not available here yet.
func Alert(string, string, bool) {}

// TerminalSetup is not available here yet.
func TerminalSetup(string) error { return errors.ErrUnsupported }
