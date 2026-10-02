//go:build !windows && !linux

package sysproc

import "os/exec"

func Hide(*exec.Cmd) {}

// StartBound relies on the service manager: launchd ends the whole process group with spinup.
func StartBound(cmd *exec.Cmd) error { return cmd.Start() }
