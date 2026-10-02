// Package shell runs other programs: attached to this terminal, captured, or as root.
package shell

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/darkyeg/spinup/internal/sysproc"
)

// Run shares this terminal, so the program can show progress and ask questions.
func Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", describe(name, args), err)
	}
	return nil
}

// Output runs the program without a window and returns what it printed; a failure carries its error output.
func Output(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	sysproc.Hide(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return string(out), fmt.Errorf("%s: %w: %s", describe(name, args), err, lastLines(stderr.String(), 5))
	}
	return string(out), nil
}

// Find locates a program on PATH.
func Find(name string) (string, bool) {
	path, err := exec.LookPath(name)
	return path, err == nil
}

func describe(name string, args []string) string {
	return strings.TrimSpace(name + " " + strings.Join(args, " "))
}

func lastLines(text string, n int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}
