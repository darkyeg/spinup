// Package sysproc starts helper processes without a console window.
package sysproc

import (
	"os/exec"
	"syscall"
)

// Hide stops a console window from appearing for cmd.
func Hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
