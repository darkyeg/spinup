package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"

	"github.com/darkyeg/spinup/internal/config"
)

//go:embed autostart/windows.ps1
var windowsScript []byte

// elevatedRequest is what the admin script does; it travels as a file, so no value is ever
// pasted into a command line.
type elevatedRequest struct {
	Action   string `json:"action"`
	Exe      string `json:"exe"`
	Home     string `json:"home"`
	User     string `json:"user"`
	Port     int    `json:"port"`
	OpenPort bool   `json:"open_port"`
	Result   string `json:"result"`
}

func registerAutostart(exe string, cfg config.Config) error {
	u, err := user.Current()
	if err != nil {
		return err
	}
	return runElevated(elevatedRequest{
		Action: "install", Exe: exe, Home: config.StateDir(), User: u.Username,
		Port: cfg.Port, OpenPort: cfg.Hold.CanHold(),
	})
}

func unregisterAutostart(config.Config) error {
	return runElevated(elevatedRequest{Action: "uninstall", Exe: installedBinary()})
}

// runElevated asks Windows for admin once (UAC) unless this process already has it. The script's
// answer comes back through a file: an elevated process can't write to this console.
func runElevated(req elevatedRequest) error {
	dir := filepath.Join(config.StateDir(), "admin")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	script := filepath.Join(dir, "spinup-admin.ps1")
	request := filepath.Join(dir, "request.json")
	req.Result = filepath.Join(dir, "result.txt")
	_ = os.Remove(req.Result)
	data, _ := json.Marshal(req)
	if err := os.WriteFile(script, windowsScript, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(request, data, 0o600); err != nil {
		return err
	}
	defer os.Remove(script)
	defer os.Remove(request)

	ranErr := elevatedPowerShell(script, request).Run()
	result, err := os.ReadFile(req.Result)
	if err != nil {
		if ranErr != nil {
			return fmt.Errorf("the admin step didn't run (declined?): %w", ranErr)
		}
		return errors.New("the admin step didn't report back")
	}
	if msg := strings.TrimSpace(string(result)); msg != "ok" {
		return fmt.Errorf("admin step: %s", msg)
	}
	return nil
}

func elevatedPowerShell(script, request string) *exec.Cmd {
	args := []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script, "-Request", request}
	if windows.GetCurrentProcessToken().IsElevated() {
		return exec.Command("powershell", args...)
	}
	step("Windows asks for admin once: for the boot task and the firewall rule")
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = "'" + strings.ReplaceAll(`"`+a+`"`, "'", "''") + "'"
	}
	return exec.Command("powershell", "-NoProfile", "-Command",
		"Start-Process powershell -Verb RunAs -Wait -WindowStyle Hidden -ArgumentList "+strings.Join(quoted, ","))
}
