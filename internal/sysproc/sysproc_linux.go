package sysproc

import (
	"os/exec"
	"runtime"
	"syscall"
)

func Hide(*exec.Cmd) {}

// StartBound asks the kernel to kill cmd when spinup exits; systemd's cgroup does it as well.
// The kernel ties that to the thread that forked, so one goroutine keeps its thread from the
// start until cmd has exited; wait returns cmd's result.
func StartBound(cmd *exec.Cmd) (wait func() error, err error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	started, exited := make(chan error, 1), make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		err := cmd.Start()
		started <- err
		if err == nil {
			exited <- cmd.Wait()
		}
	}()
	if err := <-started; err != nil {
		return nil, err
	}
	return func() error { return <-exited }, nil
}
