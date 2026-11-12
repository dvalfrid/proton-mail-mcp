//go:build !windows

package setup

import "os/exec"

// noConsole does nothing here: only Windows opens a console window for a
// child process.
func noConsole(*exec.Cmd) {}
