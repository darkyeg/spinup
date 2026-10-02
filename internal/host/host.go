// Package host knows where this OS and user keep the things spinup manages.
package host

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const Windows = runtime.GOOS == "windows"

func Home() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		panic("no home folder: " + err.Error())
	}
	return dir
}

// ClaudeHome is Claude Code's config folder.
func ClaudeHome() string { return envOr("CLAUDE_CONFIG_DIR", filepath.Join(Home(), ".claude")) }

// CodexHome is Codex's config folder.
func CodexHome() string { return envOr("CODEX_HOME", filepath.Join(Home(), ".codex")) }

// SkillsDir is the skill store every agent reads.
func SkillsDir() string { return filepath.Join(Home(), ".agents", "skills") }

// ParkedSkillsDir keeps skills no agent should load.
func ParkedSkillsDir() string { return filepath.Join(Home(), ".agents", "skills-parked") }

// BinDir holds the launchers spinup writes, such as ccp.
func BinDir() string { return filepath.Join(Home(), ".local", "bin") }

// NixOS: system packages and services come from configuration.nix, not from installers.
func NixOS() bool {
	_, err := os.Stat("/etc/NIXOS")
	return err == nil
}

// OnPath reports whether dir is one of the folders in PATH.
func OnPath(dir string) bool {
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p != "" && sameDir(p, dir) {
			return true
		}
	}
	return false
}

func sameDir(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if Windows {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
