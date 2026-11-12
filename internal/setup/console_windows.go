package setup

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// noConsole keeps Windows from opening a console window for cmd when this
// program has none, as the setup wizard doesn't (issue #96). Its output is
// still captured through pipes.
func noConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}
