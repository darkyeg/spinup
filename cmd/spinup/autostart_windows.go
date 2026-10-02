//go:build windows

package main

import (
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

const (
	taskName     = "spinup"
	firewallRule = "spinup (Tailscale only)"
	// The always-on proxy that `spinup.py hub` sets up; the service runs the proxy from now on.
	legacyTask = "CLIProxyAPI"
	legacyRule = "CLIProxyAPI (Tailscale only)"
)

// registerAutostart registers a boot task that runs the service as this user without a logon
// (S4U: no stored password, no window) and, on hub/standby, opens the peer port to the tailnet
// only. This is the one step that needs admin; it asks once (UAC).
func registerAutostart(exe string, cfg config.Config) error {
	u, err := user.Current()
	if err != nil {
		return err
	}
	home := config.StateDir()
	var b strings.Builder
	b.WriteString(stopScript(exe))
	fmt.Fprintf(&b, "if (Get-ScheduledTask -TaskName %[1]s -ErrorAction SilentlyContinue) { Disable-ScheduledTask -TaskName %[1]s | Out-Null }\n", ps(legacyTask))
	fmt.Fprintf(&b, "Get-NetFirewallRule -DisplayName %s -ErrorAction SilentlyContinue | Remove-NetFirewallRule\n", ps(legacyRule))
	fmt.Fprintf(&b, "if (Test-Path %[1]s) { Move-Item -Force %[1]s $exe }\n", ps(exe+".new"))
	if cfg.Role.Eligible() {
		fmt.Fprintf(&b, "New-NetFirewallRule -DisplayName %s -Direction Inbound -Action Allow -Protocol TCP "+
			"-LocalPort %d -RemoteAddress 100.64.0.0/10 -Program $exe -Profile Any | Out-Null\n", ps(firewallRule), cfg.Port)
	}
	fmt.Fprintf(&b, `$a = New-ScheduledTaskAction -Execute $exe -Argument %s -WorkingDirectory %s
$t = New-ScheduledTaskTrigger -AtStartup
$p = New-ScheduledTaskPrincipal -UserId %s -LogonType S4U -RunLevel Limited
$s = New-ScheduledTaskSettingsSet -ExecutionTimeLimit 0 -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) `+
		`-AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable -MultipleInstances IgnoreNew
Register-ScheduledTask -TaskName %s -Action $a -Trigger $t -Principal $p -Settings $s -Force | Out-Null
Start-ScheduledTask -TaskName %s
`, ps(`daemon --home "`+home+`"`), ps(home), ps(u.Username), ps(taskName), ps(taskName))
	return runElevated("install", exe, b.String())
}

func unregisterAutostart(config.Config) error {
	exe := filepath.Join(config.StateDir(), "bin", "spinup.exe")
	script := stopScript(exe) +
		fmt.Sprintf("if (Get-ScheduledTask -TaskName %[1]s -ErrorAction SilentlyContinue) { Unregister-ScheduledTask -TaskName %[1]s -Confirm:$false }\n", ps(taskName)) +
		fmt.Sprintf("Get-NetFirewallRule -DisplayName %s -ErrorAction SilentlyContinue | Remove-NetFirewallRule\n", ps(firewallRule))
	return runElevated("uninstall", exe, script)
}

// stopScript stops the service and any proxy (ours or the old always-on one) so the port and
// the binary are free.
func stopScript(exe string) string {
	return fmt.Sprintf(`foreach ($n in @(%s, %s)) {
  if (Get-ScheduledTask -TaskName $n -ErrorAction SilentlyContinue) { Stop-ScheduledTask -TaskName $n }
}
Get-Process spinup -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $exe } | Stop-Process -Force
Get-Process cli-proxy-api -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Milliseconds 500
`, ps(taskName), ps(legacyTask))
}

// runElevated runs a PowerShell script as admin and reports its error, if any. The script and its
// result go through files: an elevated process can't write to our console.
func runElevated(name, exe, body string) error {
	dir := filepath.Join(config.StateDir(), "admin")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	script := filepath.Join(dir, name+".ps1")
	result := filepath.Join(dir, name+".result")
	_ = os.Remove(result)
	full := fmt.Sprintf("$ErrorActionPreference = 'Stop'\n$exe = %s\ntry {\n%s\nSet-Content -LiteralPath %s -Value ok\n} catch {\nSet-Content -LiteralPath %s -Value $_.Exception.Message\n}\n",
		ps(exe), body, ps(result), ps(result))
	if err := os.WriteFile(script, []byte(full), 0o600); err != nil {
		return err
	}
	defer os.Remove(script)

	var cmd *exec.Cmd
	if windows.GetCurrentProcessToken().IsElevated() {
		cmd = exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script)
	} else {
		fmt.Println("    (Windows asks for admin once: for the boot task and the firewall rule)")
		cmd = exec.Command("powershell", "-NoProfile", "-Command",
			"Start-Process powershell -Verb RunAs -Wait -WindowStyle Hidden -ArgumentList "+
				"'-NoProfile','-ExecutionPolicy','Bypass','-File',"+ps(`"`+script+`"`))
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	runErr := cmd.Run()
	out, err := os.ReadFile(result)
	if err != nil {
		if runErr != nil {
			return fmt.Errorf("the admin step didn't run (UAC declined?): %w", runErr)
		}
		return errors.New("the admin step didn't report back")
	}
	if msg := strings.TrimSpace(string(out)); msg != "ok" {
		return fmt.Errorf("admin step: %s", msg)
	}
	return nil
}

// ps quotes a PowerShell string literal.
func ps(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
