package main

import (
	"bytes"
	_ "embed"
	"os"
	"path/filepath"
	"runtime"
	"text/template"

	"github.com/darkyeg/spinup/internal/atomicfile"
)

var (
	//go:embed launcher/ccp.cmd.tmpl
	ccpWindows string
	//go:embed launcher/ccp.sh.tmpl
	ccpUnix string
)

// writeLauncher writes `ccp`: Claude Code through the accounts, via this machine's localhost.
func writeLauncher(port int, apiKey string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	text, name, perm := ccpUnix, "ccp", os.FileMode(0o700)
	if runtime.GOOS == "windows" {
		text, name, perm = ccpWindows, "ccp.cmd", 0o600
	}
	var out bytes.Buffer
	err = template.Must(template.New(name).Parse(text)).Execute(&out, map[string]any{"Port": port, "APIKey": apiKey})
	if err != nil {
		return err
	}
	content := out.Bytes()
	if runtime.GOOS == "windows" {
		content = bytes.ReplaceAll(content, []byte("\n"), []byte("\r\n"))
	}
	return atomicfile.Write(filepath.Join(home, ".local", "bin", name), content, perm)
}
