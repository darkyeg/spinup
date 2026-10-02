package project

import (
	"context"
	"os"
	"os/exec"

	"github.com/darkyeg/spinup/internal/shell"
)

// Runner runs the programs Inspect needs; tests replace it.
type Runner interface {
	Output(ctx context.Context, name string, args ...string) (string, error)
	RunIn(ctx context.Context, dir, name string, args ...string) error
}

// SystemRunner runs real programs.
type SystemRunner struct{}

func (SystemRunner) Output(ctx context.Context, name string, args ...string) (string, error) {
	return shell.Output(ctx, name, args...)
}

func (SystemRunner) RunIn(ctx context.Context, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
