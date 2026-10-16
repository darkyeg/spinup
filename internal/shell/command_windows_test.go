package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCommandRefreshesStaleWindowsPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := exec.LookPath("cmd.exe"); err == nil {
		t.Fatal("fixture must reproduce a missing executable")
	}
	cmd := CommandContext(t.Context(), "cmd.exe", "/d", "/c", "exit", "0")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("registered executable not found from stale PATH: %v: %s", err, output)
	}
}

func TestRefreshPathDoesNotGrowOnRepeatedFailures(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	RefreshPath()
	first := os.Getenv("PATH")
	for range 10 {
		RefreshPath()
	}
	if got := os.Getenv("PATH"); got != first {
		t.Fatalf("PATH grew from %d to %d bytes", len(first), len(got))
	}
}

func TestCommandPreservesExplicitWindowsPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cmd.exe")
	if err := os.WriteFile(path, []byte("resolution fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	cmd := CommandContext(t.Context(), "cmd.exe")
	if cmd.Err != nil || cmd.Path != path || os.Getenv("PATH") != dir {
		t.Fatalf("explicit executable selection changed: %s %v", cmd.Path, cmd.Err)
	}
}

func TestMergeWindowsPathsRepairsMalformedQuotesWithoutHidingTools(t *testing.T) {
	for _, test := range []struct {
		name  string
		paths []string
		want  string
	}{
		{"unmatched quote", []string{`C:\System;C:\Vendor";C:\Tools`, `C:\Tools;C:\Other`}, `C:\System;C:\Vendor;C:\Tools;C:\Other`},
		{"separate registry values", []string{`C:\Vendor"`, `C:\Tools`}, `C:\Vendor;C:\Tools`},
		{"valid quoted semicolon", []string{`"C:\semi;colon";C:\Tools`, `"C:\semi;colon"`}, `"C:\semi;colon";C:\Tools`},
		{"valid and unmatched quotes", []string{`"C:\semi;colon";C:\Vendor";C:\Tools`}, `"C:\semi;colon";C:\Vendor;C:\Tools`},
		{"unmatched before valid quotes", []string{`C:\Vendor";"C:\semi;colon";C:\Tools`}, `C:\Vendor;"C:\semi;colon";C:\Tools`},
		{"unmatched opening quote", []string{`C:\System;"C:\Vendor;C:\Tools`}, `C:\System;C:\Vendor;C:\Tools`},
		{"case and separators", []string{`C:\Tools;`, `c:\tools\;;C:\Other`}, `C:\Tools;C:\Other`},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := mergeWindowsPaths(test.paths...)
			if got != test.want || mergeWindowsPaths(got, got) != got {
				t.Fatalf("merged PATH %q; want stable %q", got, test.want)
			}
		})
	}
}

func TestCommandFindsToolAfterMalformedPathEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spinup-path-probe.exe")
	if err := os.WriteFile(path, []byte("resolution fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir()+`";`+dir)
	if _, err := exec.LookPath("spinup-path-probe.exe"); err == nil {
		t.Fatal("fixture must reproduce the malformed PATH failure")
	}
	cmd := CommandContext(t.Context(), "spinup-path-probe.exe")
	if cmd.Err != nil || cmd.Path != path {
		t.Fatalf("tool hidden by malformed entry: %s %v", cmd.Path, cmd.Err)
	}
}
