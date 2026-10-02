package tailnet

import (
	"context"
	"strings"

	"github.com/darkyeg/spinup/internal/shell"
)

const (
	serviceState  = "(Get-Service Tailscale).StartType.ToString() + ' ' + (Get-Service Tailscale).Status.ToString()"
	wantedState   = "Automatic Running"
	startAtBootPS = "Set-Service Tailscale -StartupType Automatic; Start-Service Tailscale"
)

func startAtBoot(ctx context.Context) error {
	state, _ := shell.Output(ctx, "powershell", "-NoProfile", "-Command", serviceState)
	if strings.Join(strings.Fields(state), " ") == wantedState {
		return nil
	}
	return shell.Elevated(ctx, "Start Tailscale at boot", startAtBootPS)
}
