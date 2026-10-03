package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/darkyeg/spinup/internal/atomicfile"
	"github.com/darkyeg/spinup/internal/config"
)

//go:embed autostart/launchagent.plist
var plistTemplate string

const (
	agentLabel = "dev.spinup.daemon"
	// The always-on proxy of earlier spinup versions would hold the port.
	legacyDaemon      = "/Library/LaunchDaemons/com.spinup.cliproxyapi.plist"
	legacyDaemonLabel = "system/com.spinup.cliproxyapi"
)

// registerAutostart installs a LaunchAgent, which runs while this user is logged in.
func registerAutostart(exe string, _ config.Config) error {
	if err := writeAgent(exe); err != nil {
		return err
	}
	if _, err := os.Stat(legacyDaemon); err == nil {
		step("Turning off the old always-on proxy")
		_ = runAsRoot("launchctl", "bootout", legacyDaemonLabel)
		if err := runAsRoot("rm", legacyDaemon); err != nil {
			return err
		}
	}
	_ = run("launchctl", "bootout", agentTarget())
	return run("launchctl", "bootstrap", guiDomain(), agentPath())
}

func unregisterAutostart(config.Config) error {
	_ = run("launchctl", "bootout", agentTarget())
	if err := os.Remove(agentPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// writeAgent uses html/template for its escaping: a plist is XML.
func writeAgent(exe string) error {
	var plist bytes.Buffer
	err := template.Must(template.New(agentLabel).Parse(plistTemplate)).
		Execute(&plist, map[string]string{"Label": agentLabel, "Exe": exe, "Home": config.StateDir()})
	if err != nil {
		return err
	}
	return atomicfile.Write(agentPath(), plist.Bytes(), 0o644)
}

func agentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", agentLabel+".plist")
}

func guiDomain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }

func agentTarget() string { return guiDomain() + "/" + agentLabel }

func autostartRegistered(config.Hold) bool {
	return exec.Command("launchctl", "print", agentTarget()).Run() == nil
}
