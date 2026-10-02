//go:build darwin

package main

import (
	"fmt"
	"html"
	"os"
	"path/filepath"

	"github.com/darkyeg/spinup/internal/config"
)

const (
	agentLabel = "dev.spinup.daemon"
	// The always-on proxy that `spinup.py hub` sets up; the service runs the proxy from now on.
	legacyDaemon = "/Library/LaunchDaemons/com.spinup.cliproxyapi.plist"
)

func agentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", agentLabel+".plist")
}

func guiDomain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }

// registerAutostart installs a LaunchAgent: it runs while this user is logged in.
func registerAutostart(exe string, _ config.Config) error {
	x := html.EscapeString
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key><array>
    <string>%s</string><string>daemon</string><string>--home</string><string>%s</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardErrorPath</key><string>%s</string>
</dict></plist>
`, agentLabel, x(exe), x(config.StateDir()), x(filepath.Join(config.StateDir(), "launchd.log")))
	if err := config.WriteFileAtomic(agentPath(), []byte(plist), 0o644); err != nil {
		return err
	}
	if _, err := os.Stat(legacyDaemon); err == nil {
		step("Turning off the old always-on proxy; the service runs it now")
		_ = runSudo("launchctl", "bootout", "system/com.spinup.cliproxyapi")
		if err := runSudo("rm", legacyDaemon); err != nil {
			return err
		}
	}
	_ = run("launchctl", "bootout", guiDomain()+"/"+agentLabel)
	return run("launchctl", "bootstrap", guiDomain(), agentPath())
}

func unregisterAutostart(config.Config) error {
	_ = run("launchctl", "bootout", guiDomain()+"/"+agentLabel)
	if err := os.Remove(agentPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
