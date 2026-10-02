package shell

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// Elevated runs a PowerShell script as administrator, asking once through UAC unless spinup already
// has admin. The script's outcome comes back through a file, because an elevated process can't
// write to this console.
func Elevated(ctx context.Context, what, script string) error {
	dir, err := os.MkdirTemp(os.Getenv("LOCALAPPDATA"), "spinup-admin-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	file, result := filepath.Join(dir, "step.ps1"), filepath.Join(dir, "result.txt")
	wrapped := "param([string]$Result)\n$ErrorActionPreference = 'Stop'\ntry {\n" + script +
		"\n'ok' | Set-Content -Encoding utf8 $Result\n} catch { $_ | Out-String | Set-Content -Encoding utf8 $Result }\n"
	if err := os.WriteFile(file, []byte(wrapped), 0o600); err != nil {
		return err
	}
	ranErr := elevatedPowerShell(ctx, what, "-File", file, "-Result", result).Run()
	out, err := os.ReadFile(result)
	switch {
	case err != nil && ranErr != nil:
		return fmt.Errorf("%s: the admin step didn't run (declined?): %w", what, ranErr)
	case err != nil:
		return fmt.Errorf("%s: the admin step didn't report back", what)
	}
	if msg := strings.TrimSpace(string(bytes.TrimPrefix(out, utf8BOM))); msg != "ok" {
		return fmt.Errorf("%s: %s", what, msg)
	}
	return nil
}

func elevatedPowerShell(ctx context.Context, what string, args ...string) *exec.Cmd {
	args = append([]string{"-NoProfile", "-ExecutionPolicy", "Bypass"}, args...)
	if windows.GetCurrentProcessToken().IsElevated() {
		return exec.CommandContext(ctx, "powershell", args...)
	}
	fmt.Printf("==> %s needs admin: Windows asks once\n", what)
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = "'" + strings.ReplaceAll(`"`+a+`"`, "'", "''") + "'"
	}
	return exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command",
		"Start-Process powershell -Verb RunAs -Wait -WindowStyle Hidden -ArgumentList "+strings.Join(quoted, ","))
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}
