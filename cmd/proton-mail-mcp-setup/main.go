// Command proton-mail-mcp-setup is the Proton Mail MCP window, the one
// program the owner opens (issue #176). Before the first setup it shows the
// setup steps: find Proton Mail Bridge, pin its certificate, ask for the
// Bridge password, save everything and register proton-mail-mcp in Claude.
// Once set up, it shows the Status page (the same check as
// `proton-mail-mcp check`), Settings (issue #158) and About, and opens on
// Status when a step fails, else on Settings.
//
// With -settings it opens the Settings page without checking first.
//
// With -cleanup (run once at sign-in after an update) it shows nothing: it
// removes the copies an update renamed because Claude was running them. A
// normal start does this too.
//
// With -uninstall (run by the Windows uninstaller) it shows nothing: it
// removes proton-mail-mcp from Claude where it starts the exe next to it,
// and with -remove-settings also the settings, the stored password and the
// wizard's data.
//
// It is built on Windows as a windowed program (no console) and installed
// next to proton-mail-mcp.exe, which is what Claude starts. Without WebView2
// it opens the terminal setup (`proton-mail-mcp setup`) instead. Elsewhere it
// only points to the terminal setup for now (issue #91).
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/dvalfrid/proton-mail-mcp/internal/config"
	"github.com/dvalfrid/proton-mail-mcp/internal/keystore"
	"github.com/dvalfrid/proton-mail-mcp/internal/setup"
	"github.com/dvalfrid/proton-mail-mcp/internal/wizard"
)

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

const title = "Proton Mail MCP"

func main() {
	if err := run(); err != nil {
		wizard.Alert(title, err.Error(), true)
		_, _ = fmt.Fprintln(os.Stderr, err) // seen when started from a terminal outside Windows
		os.Exit(1)
	}
}

func run() error {
	uninstall := flag.Bool("uninstall", false, "remove proton-mail-mcp from Claude (for the uninstaller); shows nothing")
	removeSettings := flag.Bool("remove-settings", false, "with -uninstall: also remove the settings and the stored password")
	settings := flag.Bool("settings", false, "open the Settings page, without checking first")
	cleanup := flag.Bool("cleanup", false, "remove the copies an update left behind (for the installer); shows nothing")
	flag.Parse()

	server, err := serverExe(!*uninstall)
	if err != nil {
		return err
	}
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	// What an update renamed and couldn't remove while Claude ran it. A copy
	// still running stays for the next time; nothing to tell the owner.
	_, _ = setup.RemoveOldCopies(filepath.Dir(server))
	if *cleanup {
		return nil
	}
	if *uninstall {
		webview, _ := wizard.DataPath() // "" leaves it
		if err := setup.Uninstall(setup.UninstallOptions{
			Exe: server, Clients: setup.DefaultClients(), RemoveSettings: *removeSettings,
			Dir: dir, Forget: keystore.ForgetAll, WebViewDir: webview,
		}); err != nil {
			os.Exit(2) // the uninstaller goes on either way; nothing here can be shown
		}
		return nil
	}
	o := setup.Options{Store: keystore.OS{}, Dir: dir, Exe: server, Clients: setup.DefaultClients()}
	b := wizard.New(o, version, wizard.SystemAccent(), wizard.OpenURL, nil)
	if *settings {
		b.SettingsOnly()
	}
	err = wizard.Run(b, title)
	if !errors.Is(err, wizard.ErrNoWebView) {
		return err
	}
	if runtime.GOOS != "windows" {
		return fmt.Errorf("the graphical setup isn't available here yet; run `%s setup` in a terminal", server)
	}
	wizard.Alert(title, "Microsoft Edge WebView2 isn't available, so setup opens in a terminal window instead.", false)
	if err := wizard.TerminalSetup(server); err != nil {
		return fmt.Errorf("could not open the terminal setup (%w); run `%s setup` in a terminal", err, server)
	}
	return nil
}

// serverExe is proton-mail-mcp, installed next to this program: what Claude
// is told to start. mustExist is false when uninstalling, where it may
// already be gone.
func serverExe(mustExist bool) (string, error) {
	self, err := os.Executable()
	if err == nil {
		self, err = filepath.EvalSymlinks(self)
	}
	if err != nil {
		return "", fmt.Errorf("locate this program: %w", err)
	}
	name := "proton-mail-mcp"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	server := filepath.Join(filepath.Dir(self), name)
	if _, err := os.Stat(server); err != nil && mustExist {
		return "", fmt.Errorf("%s should be next to this program, but: %w", name, err)
	}
	return server, nil
}
