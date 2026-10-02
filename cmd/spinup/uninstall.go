package main

import (
	"os"

	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/leadership"
)

type uninstallCmd struct{}

func (uninstallCmd) Help() string {
	return `If this machine holds the accounts, they move to a synced hub or standby first. Without one,
the other machines wait for this one: run spinup takeover on one of them.`
}

func (uninstallCmd) Run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Hold.CanHold() {
		handOffBeforeLeaving()
	}
	stopService()
	if err := unregisterAutostart(cfg); err != nil {
		return err
	}
	step("Removed spinup from this machine. The logins and keys stay (%s, %s).", cfg.AuthDir, cfg.SecretsPath())
	if err := os.Remove(config.Path()); err != nil {
		return err
	}
	step("To bring this machine back: spinup setup <name>")
	return nil
}

func handOffBeforeLeaving() {
	service, err := keyedLocalService()
	if err != nil {
		return
	}
	r, err := service.report()
	if err != nil || !r.Leading {
		return
	}
	target := syncedHolder(r.Peers)
	if target == "" {
		step("No synced hub or standby to hand the accounts to")
		return
	}
	step("Handing the accounts to %s", target)
	if err := service.handOff(target); err != nil {
		step("The hand-off failed: %v", err)
	}
}

// syncedHolder prefers the hub among peers holding a current copy.
func syncedHolder(peers []leadership.Peer) string {
	best := ""
	for _, p := range peers {
		if p.State != leadership.Synced {
			continue
		}
		if p.Hold == config.HoldHub {
			return p.Name
		}
		if best == "" {
			best = p.Name
		}
	}
	return best
}
