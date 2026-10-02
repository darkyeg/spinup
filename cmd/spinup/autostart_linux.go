//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/darkyeg/spinup/internal/config"
)

const (
	unitName = "spinup.service"
	// The always-on proxy that `spinup.py hub` sets up; the service runs the proxy from now on.
	legacyUnit = "cliproxyapi.service"
)

func unitPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user", unitName)
}

// registerAutostart installs a systemd user unit. On hub/standby, lingering keeps it running
// without a login, so the accounts survive a reboot.
func registerAutostart(exe string, cfg config.Config) error {
	unit := fmt.Sprintf(`[Unit]
Description=spinup: AI accounts on all your machines
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=%q daemon --home %q
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
`, exe, config.StateDir())
	if err := config.WriteFileAtomic(unitPath(), []byte(unit), 0o644); err != nil {
		return err
	}
	if exec.Command("systemctl", "is-enabled", "--quiet", legacyUnit).Run() == nil ||
		exec.Command("systemctl", "is-active", "--quiet", legacyUnit).Run() == nil {
		step("Turning off the old always-on proxy (%s); the service runs it now", legacyUnit)
		if err := runSudo("systemctl", "disable", "--now", legacyUnit); err != nil {
			return err
		}
	}
	if cfg.Role.Eligible() {
		if exec.Command("loginctl", "enable-linger").Run() != nil {
			if err := runSudo("loginctl", "enable-linger", currentUser()); err != nil {
				return fmt.Errorf("keep the service running without a login: %w", err)
			}
		}
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", unitName}, {"restart", unitName}} {
		if err := run("systemctl", append([]string{"--user"}, args...)...); err != nil {
			return err
		}
	}
	return nil
}

func unregisterAutostart(config.Config) error {
	_ = run("systemctl", "--user", "disable", "--now", unitName)
	if err := os.Remove(unitPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return run("systemctl", "--user", "daemon-reload")
}
