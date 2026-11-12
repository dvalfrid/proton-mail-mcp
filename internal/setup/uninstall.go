package setup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dvalfrid/proton-mail-mcp/internal/config"
)

// UninstallOptions say what to undo when the program at Exe is removed.
type UninstallOptions struct {
	Exe     string   // the server exe being removed
	Clients []Client // where it may be registered
	// RemoveSettings also removes what setup saved: the settings and the
	// pinned certificate in Dir, the passwords (Forget), and the wizard's
	// WebView2 data in WebViewDir.
	RemoveSettings bool
	Dir            string
	Forget         func() error
	WebViewDir     string
}

// Uninstall removes the server from every client where it starts o.Exe (a
// registration for another copy is left alone), and with RemoveSettings
// what setup saved. Only files setup writes are removed, never a whole
// folder that might hold something else. It does as much as it can and
// returns every error.
func Uninstall(o UninstallOptions) error {
	var errs []error
	for _, c := range o.Clients {
		if !c.Available() {
			continue
		}
		if _, err := c.Unregister(o.Exe); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c.Name(), err))
		}
	}
	if !o.RemoveSettings {
		return errors.Join(errs...)
	}
	if o.Forget != nil {
		if err := o.Forget(); err != nil {
			errs = append(errs, err)
		}
	}
	if o.Dir != "" {
		for _, f := range []string{config.SettingsFile(o.Dir), config.CertPath(o.Dir)} {
			if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
		}
		_ = os.Remove(o.Dir) // only if now empty; anything else in it stays
	}
	if o.WebViewDir != "" {
		if err := os.RemoveAll(o.WebViewDir); err != nil {
			errs = append(errs, err)
		}
		_ = os.Remove(filepath.Dir(o.WebViewDir)) // only if now empty
	}
	return errors.Join(errs...)
}
