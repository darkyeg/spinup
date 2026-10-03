package proxy

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sync"
	"time"

	"github.com/darkyeg/spinup/internal/sysproc"
)

const (
	quietBound = 5 * time.Second
	exitBound  = 10 * time.Second
)

// Runner keeps CLIProxyAPI running while it is wanted, restarting it if it crashes.
type Runner struct {
	Exe, Dir, Config string
	Port             int
	Log              *log.Logger

	mu     sync.Mutex
	wanted bool
	cmd    *exec.Cmd
	exited chan struct{}
}

// Start returns once the proxy answers.
func (r *Runner) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	r.wanted = true
	r.mu.Unlock()
	if err := r.spawn(ctx); err != nil {
		return err
	}
	return r.waitHealthy(ctx)
}

// Stop waits up to quietBound for quiet (nil skips the wait), kills the process, and waits up to exitBound for it to exit.
func (r *Runner) Stop(quiet func() bool) {
	r.mu.Lock()
	r.wanted = false
	cmd, exited := r.cmd, r.exited
	r.mu.Unlock()
	if cmd == nil {
		return
	}
	for deadline := time.Now().Add(quietBound); quiet != nil && !quiet() && time.Now().Before(deadline); {
		time.Sleep(500 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	select {
	case <-exited:
		r.Log.Printf("proxy: stopped CLIProxyAPI")
	case <-time.After(exitBound):
		r.Log.Printf("proxy: CLIProxyAPI did not exit after kill")
	}
}

func (r *Runner) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cmd != nil
}

func (r *Runner) spawn(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !r.wanted || r.cmd != nil {
		return nil
	}
	// A proxy already answering is one spinup doesn't control, still refreshing tokens: never start a second.
	if healthy(ctx, r.base()) {
		return fmt.Errorf("a CLIProxyAPI that spinup didn't start answers on port %d; stop it first", r.Port)
	}
	cmd := exec.Command(r.Exe, "-config", r.Config)
	cmd.Dir = r.Dir
	if err := ctx.Err(); err != nil {
		return err
	}
	wait, err := sysproc.StartBound(cmd)
	if err != nil {
		return fmt.Errorf("start CLIProxyAPI: %w", err)
	}
	r.cmd, r.exited = cmd, make(chan struct{})
	r.Log.Printf("proxy: started CLIProxyAPI (pid %d)", cmd.Process.Pid)
	go r.restartOnExit(wait, r.exited)
	return nil
}

func (r *Runner) restartOnExit(wait func() error, exited chan struct{}) {
	err := wait()
	r.mu.Lock()
	r.cmd = nil
	wanted := r.wanted
	r.mu.Unlock()
	close(exited)
	if !wanted {
		return
	}
	r.Log.Printf("proxy: CLIProxyAPI exited (%v); restarting in 3s", err)
	time.Sleep(3 * time.Second)
	if err := r.spawn(context.Background()); err != nil {
		r.Log.Printf("proxy: %v", err)
	}
}

func (r *Runner) base() string { return fmt.Sprintf("http://127.0.0.1:%d", r.Port) }

func (r *Runner) waitHealthy(ctx context.Context) error {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if healthy(ctx, r.base()) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("CLIProxyAPI didn't answer on port %d within 30s (its logs are in %s)", r.Port, r.Dir)
}
