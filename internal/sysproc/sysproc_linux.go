package sysproc

import (
	"os/exec"
	"syscall"
)

func Hide(*exec.Cmd) {}

// StartBound asks the kernel to kill cmd when spinup exits; systemd's cgroup does it as well.
func StartBound(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	return cmd.Start()
}
