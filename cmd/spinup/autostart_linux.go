package main

import (
	"bytes"
	_ "embed"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/darkyeg/spinup/internal/atomicfile"
	"github.com/darkyeg/spinup/internal/config"
)

//go:embed autostart/spinup.service
var unitTemplate string

const (
	unitName = "spinup.service"
	// The always-on proxy of earlier spinup versions would hold the port.
	legacyUnit     = "cliproxyapi.service"
	legacyUnitPath = "/etc/systemd/system/" + legacyUnit
)

// registerAutostart installs a systemd user unit; lingering keeps it running without a login on machines that can hold.
func registerAutostart(exe string, cfg config.Config) error {
	if err := writeUnit(exe); err != nil {
		return err
	}
	if err := removeLegacyUnit(); err != nil {
		return err
	}
	if cfg.Hold.CanHold() && exec.Command("loginctl", "enable-linger").Run() != nil {
		if err := runAsRoot("loginctl", "enable-linger", currentUser()); err != nil {
			return err
		}
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", unitName}, {"restart", unitName}} {
		if err := run("systemctl", append([]string{"--user"}, args...)...); err != nil {
			return err
		}
	}
	return nil
}

// removeLegacyUnit turns off and deletes the old proxy unit. One the NixOS configuration declares can only go from there.
func removeLegacyUnit() error {
	if target, err := filepath.EvalSymlinks(legacyUnitPath); err == nil && strings.HasPrefix(target, "/nix/store/") {
		return errors.New("your NixOS configuration still declares the old proxy (systemd.services.cliproxyapi): " +
			"remove it, run `sudo nixos-rebuild switch`, then run setup again")
	}
	if systemctlSays("is-enabled", legacyUnit) || systemctlSays("is-active", legacyUnit) {
		step("Turning off the old always-on proxy (%s)", legacyUnit)
		if err := runAsRoot("systemctl", "disable", "--now", legacyUnit); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(legacyUnitPath); err != nil {
		return nil
	}
	step("Removing the old proxy unit (%s)", legacyUnitPath)
	if err := runAsRoot("rm", legacyUnitPath); err != nil {
		return err
	}
	return runAsRoot("systemctl", "daemon-reload")
}

func unregisterAutostart(config.Config) error {
	_ = run("systemctl", "--user", "disable", "--now", unitName)
	if err := os.Remove(unitPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return run("systemctl", "--user", "daemon-reload")
}

func writeUnit(exe string) error {
	tmpl := template.Must(template.New(unitName).Funcs(template.FuncMap{"quote": strconv.Quote}).Parse(unitTemplate))
	var unit bytes.Buffer
	if err := tmpl.Execute(&unit, map[string]string{"Exe": exe, "Home": config.StateDir()}); err != nil {
		return err
	}
	return atomicfile.Write(unitPath(), unit.Bytes(), 0o644)
}

func unitPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user", unitName)
}

func systemctlSays(question, unit string) bool {
	return exec.Command("systemctl", question, "--quiet", unit).Run() == nil
}

func autostartRegistered() bool {
	return exec.Command("systemctl", "--user", "is-enabled", "--quiet", unitName).Run() == nil
}
