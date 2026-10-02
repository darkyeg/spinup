//go:build !windows

package shell

import (
	"context"
	"os"
)

// AsRoot runs the program through sudo unless spinup already runs as root.
func AsRoot(ctx context.Context, name string, args ...string) error {
	if os.Geteuid() == 0 {
		return Run(ctx, name, args...)
	}
	return Run(ctx, "sudo", append([]string{name}, args...)...)
}

// RefreshPath is only needed on Windows.
func RefreshPath() {}
