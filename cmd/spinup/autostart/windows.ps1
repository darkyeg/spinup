# Runs elevated, once per spinup install or uninstall: the boot task and the firewall rule need admin.
param([Parameter(Mandatory)][string]$Request)
$ErrorActionPreference = 'Stop'
$r = Get-Content -Raw -LiteralPath $Request | ConvertFrom-Json

function Stop-Task($name) {
    if (Get-ScheduledTask -TaskName $name -ErrorAction SilentlyContinue) { Stop-ScheduledTask -TaskName $name }
}

function Remove-Rule($name) {
    Get-NetFirewallRule -DisplayName $name -ErrorAction SilentlyContinue | Remove-NetFirewallRule
}

try {
    Stop-Task 'spinup'
    Get-Process spinup -ErrorAction SilentlyContinue | Where-Object Path -eq $r.exe | Stop-Process -Force
    Get-Process cli-proxy-api -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Milliseconds 500
    Remove-Rule 'spinup (Tailscale only)'

    if ($r.action -eq 'uninstall') {
        if (Get-ScheduledTask -TaskName 'spinup' -ErrorAction SilentlyContinue) {
            Unregister-ScheduledTask -TaskName 'spinup' -Confirm:$false
        }
    } else {
        # An always-on proxy from an earlier setup would hold the port.
        Stop-Task 'CLIProxyAPI'
        if (Get-ScheduledTask -TaskName 'CLIProxyAPI' -ErrorAction SilentlyContinue) {
            Unregister-ScheduledTask -TaskName 'CLIProxyAPI' -Confirm:$false
        }
        Remove-Rule 'CLIProxyAPI (Tailscale only)'

        $staged = "$($r.exe).new"
        if (Test-Path -LiteralPath $staged) { Move-Item -Force -LiteralPath $staged -Destination $r.exe }
        if ($r.open_port) {
            New-NetFirewallRule -DisplayName 'spinup (Tailscale only)' -Direction Inbound -Action Allow -Protocol TCP `
                -LocalPort $r.port -RemoteAddress 100.64.0.0/10 -Program $r.exe -Profile Any | Out-Null
        }
        # S4U: runs as this user at boot, before anyone logs in, without a stored password or a window.
        $action = New-ScheduledTaskAction -Execute $r.exe -Argument "daemon --home `"$($r.home)`"" -WorkingDirectory $r.home
        $trigger = New-ScheduledTaskTrigger -AtStartup
        $principal = New-ScheduledTaskPrincipal -UserId $r.user -LogonType S4U -RunLevel Limited
        $settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit 0 -RestartCount 999 `
            -RestartInterval (New-TimeSpan -Minutes 1) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
            -StartWhenAvailable -MultipleInstances IgnoreNew
        Register-ScheduledTask -TaskName 'spinup' -Action $action -Trigger $trigger -Principal $principal `
            -Settings $settings -Force | Out-Null
        Start-ScheduledTask -TaskName 'spinup'
    }
    Set-Content -LiteralPath $r.result -Value 'ok'
} catch {
    Set-Content -LiteralPath $r.result -Value $_.Exception.Message
}
