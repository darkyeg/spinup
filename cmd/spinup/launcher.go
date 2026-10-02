package main

import (
	"bytes"
	_ "embed"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"text/template"

	"github.com/darkyeg/spinup/internal/atomicfile"
	"github.com/darkyeg/spinup/internal/host"
)

var (
	//go:embed launcher/ccp.cmd.tmpl
	ccpWindows string
	//go:embed launcher/ccp.sh.tmpl
	ccpUnix string
)

var launcherSafe = regexp.MustCompile(`^[A-Za-z0-9._~+/=-]+$`)

// checkLauncherKey refuses keys with characters that could break out of the launcher script.
func checkLauncherKey(key string) error {
	if !launcherSafe.MatchString(key) {
		return errors.New("the API key may only have letters, digits and . _ ~ + / = -")
	}
	return nil
}

// writeLauncher writes `ccp`: Claude Code through the accounts, via this machine's localhost.
func writeLauncher(port int, apiKey string) error {
	if err := checkLauncherKey(apiKey); err != nil {
		return err
	}
	text, name, perm := ccpUnix, "ccp", os.FileMode(0o700)
	if runtime.GOOS == "windows" {
		text, name, perm = ccpWindows, "ccp.cmd", 0o600
	}
	var out bytes.Buffer
	err := template.Must(template.New(name).Parse(text)).Execute(&out, map[string]any{"Port": port, "APIKey": apiKey})
	if err != nil {
		return err
	}
	content := out.Bytes()
	if runtime.GOOS == "windows" {
		content = bytes.ReplaceAll(content, []byte("\n"), []byte("\r\n"))
	}
	if err := atomicfile.Write(filepath.Join(host.BinDir(), name), content, perm); err != nil {
		return err
	}
	if err := ensureOnPath(host.BinDir()); err != nil {
		step("Couldn't put %s on your PATH (%v); add it yourself so `ccp` is found", host.BinDir(), err)
	}
	return nil
}
