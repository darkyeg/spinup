// Package config holds this machine's spinup settings and where spinup keeps its files.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/darkyeg/spinup/internal/atomicfile"
)

// Hold says whether this machine may hold the accounts. Every machine uses them.
type Hold string

const (
	HoldNever   Hold = "never"
	HoldStandby Hold = "standby" // takes the accounts while the hub is gone
	HoldHub     Hold = "hub"     // holds the accounts whenever it is up
)

// CanHold reports whether the machine may run the accounts.
func (h Hold) CanHold() bool { return h == HoldStandby || h == HoldHub }

func (h Hold) known() bool { return h == HoldNever || h.CanHold() }

type Config struct {
	Hold Hold `json:"hold"`
	// Port serves localhost and, on machines that can hold, the Tailscale address.
	Port int `json:"port"`
	// ProxyPort is CLIProxyAPI's own port, on 127.0.0.1 only.
	ProxyPort            int    `json:"proxy_port"`
	FailoverAfterSeconds int    `json:"failover_after_seconds"`
	AutoFailback         bool   `json:"auto_failback"`
	ProxyDir             string `json:"proxy_dir"`
	AuthDir              string `json:"auth_dir"`
	// Tailscale is the tailscale CLI; empty means search for it.
	Tailscale string `json:"tailscale,omitempty"`
	// Repo is the spinup checkout setup ran from; empty means the data built into the binary.
	Repo string `json:"repo,omitempty"`
}

// FailoverAfter is how long Tailscale must report the leader offline before another machine leads.
func (c Config) FailoverAfter() time.Duration {
	return time.Duration(c.FailoverAfterSeconds) * time.Second
}

func (c Config) ProxyExe() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(c.ProxyDir, "cli-proxy-api.exe")
	}
	return filepath.Join(c.ProxyDir, "cli-proxy-api")
}

func (c Config) ProxyConfig() string { return filepath.Join(c.ProxyDir, "config.yaml") }

// RemovedDir keeps logins that disappeared from the leader; they are never deleted.
func (c Config) RemovedDir() string { return c.AuthDir + "-removed" }

// StateDir holds config.json, state.json, the log and spinup's own binary.
func StateDir() string {
	if dir := os.Getenv("SPINUP_HOME"); dir != "" {
		return dir
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "spinup")
	}
	return filepath.Join(home(), ".local", "share", "spinup")
}

func Path() string { return filepath.Join(StateDir(), "config.json") }

// Defaults is a fresh machine's configuration.
func Defaults() Config {
	return Config{
		Hold:                 HoldNever,
		Port:                 8317,
		ProxyPort:            8327,
		FailoverAfterSeconds: 180,
		AutoFailback:         true,
		ProxyDir:             defaultProxyDir(),
		AuthDir:              filepath.Join(home(), ".cli-proxy-api"),
	}
}

func defaultProxyDir() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "CLIProxyAPI")
	case "darwin":
		return filepath.Join(home(), "Library", "Application Support", "CLIProxyAPI")
	}
	return filepath.Join(home(), ".local", "share", "cliproxyapi")
}

// minFailoverAfter is the shortest safe failover time: a machine may need up to two minutes to notice it lost
// Tailscale (its control client's watchdog), then stops ten seconds later; the rest is margin.
const minFailoverAfter = 150 * time.Second

var ErrNotInstalled = errors.New("spinup isn't installed on this machine: run `spinup setup <name>`")

// Load reads config.json over the defaults.
func Load() (Config, error) {
	c := Defaults()
	data, err := os.ReadFile(Path())
	if errors.Is(err, os.ErrNotExist) {
		return c, ErrNotInstalled
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("%s: %w", Path(), err)
	}
	if !c.Hold.known() {
		return c, fmt.Errorf("%s: unknown hold %q", Path(), c.Hold)
	}
	if c.FailoverAfter() < minFailoverAfter {
		return c, fmt.Errorf("%s: failover_after_seconds must be at least %d, or two machines could hold the accounts at once",
			Path(), int(minFailoverAfter.Seconds()))
	}
	return c, nil
}

func Save(c Config) error {
	data, _ := json.MarshalIndent(c, "", "  ")
	return atomicfile.Write(Path(), append(data, '\n'), 0o600)
}

func home() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		panic("no home folder: " + err.Error())
	}
	return dir
}
