package main

import (
	"bytes"
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"text/template"

	"github.com/darkyeg/spinup/internal/atomicfile"
	"github.com/darkyeg/spinup/internal/config"
)

//go:embed autostart/spinup.service
var unitTemplate string

const (
	unitName = "spinup.service"
	// The always-on proxy of earlier spinup versions would hold the port.
	legacyUnit = "cliproxyapi.service"
)

// registerAutostart installs a systemd user unit; lingering keeps it running without a login on machines that can hold.
func registerAutostart(exe string, cfg config.Config) error {
	if err := writeUnit(exe); err != nil {
		return err
	}
	if systemctlSays("is-enabled", legacyUnit) || systemctlSays("is-active", legacyUnit) {
		step("Turning off the old always-on proxy (%s)", legacyUnit)
		if err := runAsRoot("systemctl", "disable", "--now", legacyUnit); err != nil {
			return err
		}
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
