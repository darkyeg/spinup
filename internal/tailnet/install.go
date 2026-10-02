package tailnet

import (
	"context"
	"errors"
	"runtime"

	"github.com/darkyeg/spinup/internal/host"
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

// Update brings Tailscale to its latest version with the tool that installed it.
func Update(ctx context.Context) error {
	switch {
	case host.NixOS():
		return errors.New("on NixOS Tailscale updates with the system (nixos-rebuild)")
	case runtime.GOOS == "windows":
		return shell.Run(ctx, "winget", "upgrade", "--id", "Tailscale.Tailscale", "-e", "--silent",
			"--accept-package-agreements", "--accept-source-agreements")
	case runtime.GOOS == "darwin":
		return shell.Run(ctx, "brew", "upgrade", "tailscale")
	}
	return shell.AsRoot(ctx, Find(), "update", "--yes")
}
