package shell

import (
	"context"
	"errors"
	"os/exec"
)

func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	if errors.Is(cmd.Err, exec.ErrNotFound) {
		RefreshPath()
		cmd = exec.CommandContext(ctx, name, args...)
	}
	return cmd
}
