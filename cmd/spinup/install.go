package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/proxy"
	"github.com/darkyeg/spinup/internal/tailnet"
)

func cmdInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	role := fs.String("role", "", "hub, standby or client")
	key := fs.String("key", "", "standby: the dashboard password; client: the API key (asked if omitted)")
	_ = fs.Parse(args)

	cfg, err := config.Load()
	if err != nil && !errors.Is(err, config.ErrNotInstalled) {
		return err
	}
	if *role != "" {
		cfg.Role = config.Role(*role)
	} else if errors.Is(err, config.ErrNotInstalled) {
		return errors.New("choose a role: spinup install --role hub|standby|client")
	}
	if !cfg.Role.Valid() {
		return fmt.Errorf("unknown role %q: use hub, standby or client", cfg.Role)
	}
	if cfg.Tailscale == "" {
		cfg.Tailscale = tailnet.Find()
	}
	ts, err := tailnet.CLI{Bin: cfg.Tailscale}.Status(context.Background())
	if err != nil || !ts.Running {
		return fmt.Errorf("Tailscale isn't running on this machine (%v): install it and log in first", err)
	}
	step("This machine is %s on your tailnet; installing as %s", ts.Self.Name, cfg.Role)

	// Keys. The hub makes them; a standby fetches them from the machine holding the accounts.
	var apiKey string
	switch cfg.Role {
	case config.RoleHub:
		sec, err := config.LoadSecrets(cfg)
		if err != nil {
			sec = config.NewSecrets()
			if err := config.SaveSecrets(cfg, sec); err != nil {
				return err
			}
			step("Made new keys in %s", cfg.SecretsPath())
		} else {
			step("Using the existing keys in %s", cfg.SecretsPath())
		}
		apiKey = sec.APIKey
	case config.RoleStandby:
		sec, err := config.LoadSecrets(cfg)
		if err != nil {
			pw := *key
			if pw == "" {
				pw = ask("Dashboard password (on the hub: spinup show-key, or `spinup.py show-key`): ")
			}
			if sec, err = fetchSecrets(ts, cfg.Port, pw); err != nil {
				return err
			}
			if err := config.SaveSecrets(cfg, sec); err != nil {
				return err
			}
			step("Got the keys from the machine holding the accounts")
		}
		apiKey = sec.APIKey
	case config.RoleClient:
		apiKey = *key
		if apiKey == "" {
			apiKey = ask("API key (on the hub: spinup.py show-key): ")
		}
	}

	if cfg.Role.Eligible() && !proxy.Installed(cfg) {
		rel, err := proxy.LatestRelease(context.Background())
		if err != nil {
			return fmt.Errorf("find the latest CLIProxyAPI: %w", err)
		}
		step("Installing CLIProxyAPI %s (checksum-verified)", rel.Version)
		if err := proxy.Install(context.Background(), cfg, rel); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(cfg.AuthDir, 0o700); err != nil {
		return err
	}
	if err := config.Save(cfg); err != nil {
		return err
	}

	exe, err := stageBinary()
	if err != nil {
		return err
	}
	step("Starting the service at boot")
	if err := registerAutostart(exe, cfg); err != nil {
		return err
	}
	if err := writeCCP(cfg.Port, apiKey); err != nil {
		return err
	}

	step("Waiting for the service")
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if err := callService(http.MethodGet, frontURL(cfg, "/spinup/leader"), "", nil, nil); err == nil {
			fmt.Println()
			return cmdStatus(nil)
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("the service didn't start; see %s", filepath.Join(config.StateDir(), "spinup.log"))
}

func cmdUninstall([]string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := unregisterAutostart(cfg); err != nil {
		return err
	}
	step("Removed the service. Your logins and keys are kept (%s, %s).", cfg.AuthDir, cfg.SecretsPath())
	if cfg.Role == config.RoleHub {
		step("To go back to the Python setup's always-on proxy: py spinup.py hub")
	}
	return os.Remove(config.Path())
}

// fetchSecrets asks the machine that holds the accounts for the keys (with the dashboard password).
func fetchSecrets(ts tailnet.Status, port int, password string) (config.Secrets, error) {
	var sec config.Secrets
	for _, p := range ts.Peers {
		if !p.Online {
			continue
		}
		base := fmt.Sprintf("http://%s:%d", p.IP, port)
		var li struct {
			IsLeader bool `json:"is_leader"`
		}
		if callService(http.MethodGet, base+"/spinup/leader", "", nil, &li) != nil || !li.IsLeader {
			continue
		}
		if err := callService(http.MethodGet, base+"/spinup/secrets", password, nil, &sec); err != nil {
			return sec, fmt.Errorf("%s refused: %w (wrong password?)", p.Name, err)
		}
		if sec.APIKey == "" || sec.ManagementPassword == "" {
			return sec, fmt.Errorf("%s sent empty keys", p.Name)
		}
		return sec, nil
	}
	return sec, errors.New("no machine on your tailnet holds the accounts with the spinup service: install the hub first")
}

// stageBinary copies this program to the state folder, so the service doesn't depend on where it
// was downloaded. On Windows a running copy can't be replaced, so it is staged as .new and swapped
// in by the (elevated) autostart step.
func stageBinary() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(config.StateDir(), "bin")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := "spinup"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dest := filepath.Join(dir, name)
	if same(self, dest) {
		return dest, nil
	}
	in, err := os.Open(self)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.OpenFile(dest+".new", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	if runtime.GOOS != "windows" {
		return dest, os.Rename(dest+".new", dest)
	}
	return dest, nil
}

func same(a, b string) bool {
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(fa, fb)
}

// writeCCP writes the `ccp` launcher: Claude Code through the accounts, via this machine's front.
func writeCCP(port int, apiKey string) error {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	if runtime.GOOS == "windows" {
		body := "@echo off\r\nrem Claude Code through your accounts (generated by spinup)\r\n" +
			`set "ANTHROPIC_BASE_URL=` + url + "\"\r\n" + `set "ANTHROPIC_AUTH_TOKEN=` + apiKey + "\"\r\n" +
			"set \"ANTHROPIC_API_KEY=\"\r\nclaude %*\r\n"
		return config.WriteFileAtomic(filepath.Join(dir, "ccp.cmd"), []byte(body), 0o600)
	}
	body := "#!/bin/sh\n# Claude Code through your accounts (generated by spinup)\n" +
		`export ANTHROPIC_BASE_URL="` + url + "\"\n" + `export ANTHROPIC_AUTH_TOKEN="` + apiKey + "\"\n" +
		"unset ANTHROPIC_API_KEY\nexec claude \"$@\"\n"
	return config.WriteFileAtomic(filepath.Join(dir, "ccp"), []byte(body), 0o700)
}

func ask(prompt string) string {
	fmt.Print(prompt)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, _ := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		return strings.TrimSpace(string(b))
	}
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line)
}

func step(format string, a ...any) { fmt.Printf("==> "+format+"\n", a...) }
