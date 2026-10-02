package tailnet

import (
	"context"
	"errors"
	"runtime"

	"github.com/darkyeg/spinup/internal/shell"
)

const installScript = "curl -fsSL https://tailscale.com/install.sh | sh"

func install(ctx context.Context) error {
	switch runtime.GOOS {
	case "windows":
		return shell.Run(ctx, "winget", "install", "--id", "Tailscale.Tailscale", "-e", "--silent",
			"--accept-package-agreements", "--accept-source-agreements")
	case "darwin":
		if _, ok := shell.Find("brew"); !ok {
			return errors.New("install Homebrew first: https://brew.sh")
		}
		return shell.Run(ctx, "brew", "install", "tailscale")
	}
	return shell.Run(ctx, "sh", "-c", installScript)
}
