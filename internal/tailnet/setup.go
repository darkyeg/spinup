package tailnet

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkyeg/spinup/internal/host"
	"github.com/darkyeg/spinup/internal/shell"
)

const (
	nixOSSetup = `NixOS: add this to /etc/nixos/configuration.nix, then run ` + "`sudo nixos-rebuild switch`" + `:

  services.tailscale.enable = true;

Then run ` + "`sudo tailscale up`" + ` once to log in, and re-run this command.`

	noState         = "NoState"
	backendWaits    = 10
	backendWaitStep = time.Second
)

// Setup installs Tailscale, starts it at boot, logs in, and with a name makes this machine reachable as that name.
func Setup(ctx context.Context, name string) error {
	if name != "" && !ValidName(name) {
		return fmt.Errorf("%q is not a machine name: use lowercase letters, digits and dashes, up to 63", name)
	}
	if err := ensureInstalled(ctx); err != nil {
		return err
	}
	if !host.NixOS() {
		if err := startAtBoot(ctx); err != nil {
			return err
		}
	}
	bin := Find()
	if bin == "" {
		return errors.New("tailscale not found after install")
	}
	cli := CLI{Bin: bin}
	st := waitForBackend(ctx, cli)
	if !st.Running {
		fmt.Println("==> Log in to Tailscale (open the URL it prints): use the same account on every machine")
		if err := shell.AsRoot(ctx, bin, upArgs(name, host.Windows)...); err != nil {
			return err
		}
	} else if host.Windows {
		_, _ = shell.Output(ctx, bin, "set", "--unattended")
	}
	st, err := cli.Status(ctx)
	if err != nil {
		return err
	}
	if name != "" && st.Self.Name != name {
		if err := shell.AsRoot(ctx, bin, "set", "--hostname="+name); err != nil {
			return err
		}
		st, _ = cli.Status(ctx)
	}
	fmt.Printf("==> Tailscale up: %s %s\n", st.Self.Name, st.Self.IP)
	if note := hostnameNote(name, st.Self.Name); note != "" {
		fmt.Println("==> " + note)
	}
	return nil
}

func hostnameNote(wanted, actual string) string {
	if wanted == "" || wanted == actual {
		return ""
	}
	return fmt.Sprintf("Tailscale named this machine %s, not %s: another device probably has that name. "+
		"Other machines must use %s; to rename it, remove the other device in the Tailscale admin console and re-run setup.", actual, wanted, actual)
}

func ensureInstalled(ctx context.Context) error {
	if host.NixOS() {
		if _, err := shell.Output(ctx, "systemctl", "is-enabled", "tailscaled"); Find() == "" || err != nil {
			return errors.New(nixOSSetup)
		}
		return nil
	}
	if Find() != "" {
		return nil
	}
	fmt.Println("==> Installing Tailscale")
	if err := install(ctx); err != nil {
		return err
	}
	shell.RefreshPath()
	return nil
}

func waitForBackend(ctx context.Context, cli CLI) Status {
	var st Status
	for range backendWaits {
		st, _ = cli.Status(ctx)
		if st.BackendState != "" && st.BackendState != noState {
			break
		}
		select {
		case <-ctx.Done():
			return st
		case <-time.After(backendWaitStep):
		}
	}
	return st
}

func upArgs(name string, windows bool) []string {
	args := []string{"up"}
	if windows {
		args = append(args, "--unattended")
	}
	if name != "" {
		args = append(args, "--hostname="+name)
	}
	return args
}
