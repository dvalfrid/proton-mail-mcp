//go:build !windows

package save

// markOfTheWeb does nothing outside Windows yet. Linux has no equivalent;
// macOS has the quarantine attribute, to be added with macOS support (0.5).
func markOfTheWeb(string) error { return nil }
