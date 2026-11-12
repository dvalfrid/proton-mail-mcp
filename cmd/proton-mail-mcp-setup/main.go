// Command proton-mail-mcp-setup is the graphical setup wizard: it opens a
// window that finds Proton Mail Bridge, pins its certificate, asks for the
// Bridge password, saves everything and registers proton-mail-mcp in Claude.
//
// It is built on Windows as a windowed program (no console) and installed
// next to proton-mail-mcp.exe, which is what Claude starts. Without WebView2
// it opens the terminal setup (`proton-mail-mcp setup`) instead. Elsewhere it
// only points to the terminal setup for now (issue #91).
package main

import (
	"errors"
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

const title = "Proton Mail MCP Setup"

func main() {
	if err := run(); err != nil {
		wizard.Alert(title, err.Error(), true)
		_, _ = fmt.Fprintln(os.Stderr, err) // seen when started from a terminal outside Windows
		os.Exit(1)
	}
}

func run() error {
	server, err := serverExe()
	if err != nil {
		return err
	}
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	o := setup.Options{Store: keystore.OS{}, Dir: dir, Exe: server, Clients: setup.DefaultClients()}
	b := wizard.New(o, version, wizard.SystemAccent(), wizard.OpenURL, nil)
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
// is told to start.
func serverExe() (string, error) {
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
	if _, err := os.Stat(server); err != nil {
		return "", fmt.Errorf("%s should be next to this program, but: %w", name, err)
	}
	return server, nil
}
