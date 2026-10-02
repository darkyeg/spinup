// Package config holds the service's settings, paths and the proxy secrets.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Role is what a machine does in the cluster.
type Role string

const (
	// RoleHub is the preferred leader: it runs the accounts whenever it is up.
	RoleHub Role = "hub"
	// RoleStandby keeps a synced copy of the accounts and takes over when the leader is gone.
	RoleStandby Role = "standby"
	// RoleClient only uses the accounts through the leader; it never holds them.
	RoleClient Role = "client"
)

// Eligible reports whether the role may hold the accounts.
func (r Role) Eligible() bool { return r == RoleHub || r == RoleStandby }

// Valid reports whether r is a known role.
func (r Role) Valid() bool { return r == RoleHub || r == RoleStandby || r == RoleClient }

// Config is stored as JSON in StateDir()/config.json.
type Config struct {
	Role Role `json:"role"`
	// Port is the front on localhost and, on hub/standby, the peer port on the Tailscale IP.
	Port int `json:"port"`
	// ProxyPort is CLIProxyAPI's own port; it only listens on 127.0.0.1.
	ProxyPort int `json:"proxy_port"`
	// FailoverAfterSeconds: how long Tailscale must report the leader offline before a standby takes over.
	FailoverAfterSeconds int `json:"failover_after_seconds"`
	// AutoFailback: a standby that is leader hands the accounts back to the hub once the hub is synced.
	AutoFailback bool   `json:"auto_failback"`
	ProxyDir     string `json:"proxy_dir"`
	AuthDir      string `json:"auth_dir"`
	// Tailscale is the tailscale CLI; empty means "find it".
	Tailscale string `json:"tailscale,omitempty"`
}

// FailoverAfter is FailoverAfterSeconds as a duration.
func (c Config) FailoverAfter() time.Duration {
	return time.Duration(c.FailoverAfterSeconds) * time.Second
}

// ProxyExe is the CLIProxyAPI binary.
func (c Config) ProxyExe() string {
	name := "cli-proxy-api"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(c.ProxyDir, name)
}

// ProxyConfig is the rendered CLIProxyAPI config.
func (c Config) ProxyConfig() string { return filepath.Join(c.ProxyDir, "config.yaml") }

// SecretsPath holds the proxy API key and management password.
func (c Config) SecretsPath() string { return filepath.Join(c.ProxyDir, "secrets.json") }

// RemovedDir receives logins that disappeared from the leader, instead of deleting them.
func (c Config) RemovedDir() string { return c.AuthDir + "-removed" }

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		panic("no home directory: " + err.Error())
	}
	return h
}

// StateDir is where the service keeps config.json, state.json, its log and its own binary.
func StateDir() string {
	if v := os.Getenv("SPINUP_HOME"); v != "" {
		return v
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "spinup")
	}
	return filepath.Join(home(), ".local", "share", "spinup")
}

// Path is the config file.
func Path() string { return filepath.Join(StateDir(), "config.json") }

// Defaults match the Python spinup.py paths, so both manage the same proxy install.
func Defaults() Config {
	proxyDir := filepath.Join(home(), ".local", "share", "cliproxyapi")
	switch runtime.GOOS {
	case "windows":
		proxyDir = filepath.Join(os.Getenv("LOCALAPPDATA"), "CLIProxyAPI")
	case "darwin":
		proxyDir = filepath.Join(home(), "Library", "Application Support", "CLIProxyAPI")
	}
	return Config{
		Role:                 RoleClient,
		Port:                 8317,
		ProxyPort:            8327,
		FailoverAfterSeconds: 180,
		AutoFailback:         true,
		ProxyDir:             proxyDir,
		AuthDir:              filepath.Join(home(), ".cli-proxy-api"),
	}
}

// ErrNotInstalled means there is no config.json yet.
var ErrNotInstalled = errors.New("spinup service is not installed on this machine (run `spinup install`)")

// Load reads config.json, filling missing fields with defaults.
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
	if !c.Role.Valid() {
		return c, fmt.Errorf("%s: unknown role %q", Path(), c.Role)
	}
	return c, nil
}

// Save writes config.json.
func Save(c Config) error {
	data, _ := json.MarshalIndent(c, "", "  ")
	return WriteFileAtomic(Path(), append(data, '\n'), 0o600)
}

// Secrets are the proxy's API key (for clients) and management password (dashboard, and the
// key machines use to talk to each other). Same format as spinup.py's secrets.json.
type Secrets struct {
	APIKey             string `json:"api_key"`
	ManagementPassword string `json:"management_password"`
}

// LoadSecrets reads secrets.json.
func LoadSecrets(c Config) (Secrets, error) {
	var s Secrets
	data, err := os.ReadFile(c.SecretsPath())
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("%s: %w", c.SecretsPath(), err)
	}
	if s.APIKey == "" || s.ManagementPassword == "" {
		return s, fmt.Errorf("%s: missing api_key or management_password", c.SecretsPath())
	}
	return s, nil
}

// SaveSecrets writes secrets.json, readable by this user only.
func SaveSecrets(c Config, s Secrets) error {
	data, _ := json.MarshalIndent(s, "", "  ")
	return WriteFileAtomic(c.SecretsPath(), append(data, '\n'), 0o600)
}

// NewSecrets makes fresh random keys.
func NewSecrets() Secrets {
	return Secrets{APIKey: "sk-" + randomHex(24), ManagementPassword: randomHex(24)}
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// WriteFileAtomic writes via a temp file in the same folder and a rename, so readers never see
// half a file. The temp name doesn't end in .json, so CLIProxyAPI's folder watcher ignores it.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		return err
	}
	return os.Rename(name, path)
}
