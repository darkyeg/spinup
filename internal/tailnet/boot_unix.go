//go:build !windows

package tailnet

import (
	"context"
	"os"
	"runtime"

	"github.com/darkyeg/spinup/internal/shell"
)

const brewLaunchDaemon = "/Library/LaunchDaemons/homebrew.mxcl.tailscale.plist"

func startAtBoot(ctx context.Context) error {
	if runtime.GOOS == "darwin" {
		if _, ok := shell.Find("brew"); !ok {
			return nil
		}
		if _, err := os.Stat(brewLaunchDaemon); err == nil {
			return nil
		}
		return shell.AsRoot(ctx, "brew", "services", "start", "tailscale")
	}
	return shell.AsRoot(ctx, "systemctl", "enable", "--now", "tailscaled")
}
