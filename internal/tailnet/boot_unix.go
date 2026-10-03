//go:build !windows

package tailnet

import (
	"context"
	"os"
	"runtime"
	"strings"

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
	if state, err := shell.Output(ctx, "systemctl", "is-enabled", "tailscaled"); err == nil && strings.TrimSpace(state) == "enabled" {
		if _, err := shell.Output(ctx, "systemctl", "is-active", "--quiet", "tailscaled"); err == nil {
			return nil
		}
	}
	return shell.AsRoot(ctx, "systemctl", "enable", "--now", "tailscaled")
}
