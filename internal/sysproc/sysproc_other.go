//go:build !windows && !linux

package sysproc

import "os/exec"

func Hide(*exec.Cmd) {}

// StartBound relies on the service manager: launchd ends the whole process group with spinup.
func StartBound(cmd *exec.Cmd) (wait func() error, err error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd.Wait, nil
}
