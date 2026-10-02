package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/proxy"
	"github.com/darkyeg/spinup/internal/release"
	"github.com/darkyeg/spinup/internal/tailnet"
)

type installCmd struct {
	Hub      bool   `xor:"hold" help:"This machine normally holds the accounts. One hub per tailnet."`
	Standby  bool   `xor:"hold" help:"This machine takes the accounts while the hub is off."`
	Password string `env:"SPINUP_PASSWORD" placeholder:"PASSWORD" help:"The dashboard password, for --standby (or $SPINUP_PASSWORD; asked when omitted; on the hub: spinup.py show-key)."`
	APIKey   string `name:"api-key" env:"SPINUP_API_KEY" placeholder:"KEY" help:"The API key, for a machine that only uses the accounts (or $SPINUP_API_KEY; asked when omitted)."`
}

func (c installCmd) Help() string {
	return `Without --hub or --standby, this machine only uses the accounts. Re-running install keeps the
machine's hold and repairs everything else.`
}

func (c installCmd) Run() error {
	cfg, err := config.Load()
	if err != nil && !errors.Is(err, config.ErrNotInstalled) {
		return err
	}
	if hold := c.hold(); hold != "" || errors.Is(err, config.ErrNotInstalled) {
		cfg.Hold = cmp.Or(hold, config.HoldNever)
	}
	if cfg.Tailscale == "" {
		cfg.Tailscale = tailnet.Find()
	}
	ts, err := tailnet.CLI{Bin: cfg.Tailscale}.Status(context.Background())
	if err := tailscaleProblem(ts, err); err != nil {
		return err
	}
	step("This machine is %s on your tailnet; it %s", ts.Self.Name, holdPhrase(cfg.Hold))

	apiKey, err := c.keys(cfg, ts)
	if err != nil {
		return err
	}
	if err := checkLauncherKey(apiKey); err != nil {
		return err
	}
	if err := ensureProxy(cfg); err != nil {
		return err
	}
	if err := config.Save(cfg); err != nil {
		return err
	}
	exe, err := stageBinary()
	if err != nil {
		return err
	}
	stopService()
	step("Starting spinup at boot")
	if err := registerAutostart(exe, cfg); err != nil {
		return err
	}
	if err := writeLauncher(cfg.Port, apiKey); err != nil {
		return err
	}
	if err := waitForService(cfg); err != nil {
		return err
	}
	fmt.Println()
	return statusCmd{}.Run()
}

func (c installCmd) hold() config.Hold {
	switch {
	case c.Hub:
		return config.HoldHub
	case c.Standby:
		return config.HoldStandby
	}
	return ""
}

func tailscaleProblem(ts tailnet.Status, err error) error {
	switch {
	case err != nil:
		return fmt.Errorf("can't read Tailscale's status (%w): install it and log in first", err)
	case !ts.Running:
		return errors.New("Tailscale is installed but not running: start it and log in first")
	}
	return nil
}

func holdPhrase(h config.Hold) string {
	switch h {
	case config.HoldHub:
		return "normally holds the accounts (hub)"
	case config.HoldStandby:
		return "takes the accounts while the hub is off (standby)"
	}
	return "uses the accounts"
}

func ensureProxy(cfg config.Config) error {
	if !cfg.Hold.CanHold() || proxy.Installed(cfg) {
		return nil
	}
	ctx := context.Background()
	rel, err := release.Latest(ctx, proxy.Project)
	if err != nil {
		return fmt.Errorf("find the latest CLIProxyAPI: %w", err)
	}
	step("Installing CLIProxyAPI %s (checksum-verified)", rel.Version)
	return proxy.Install(ctx, cfg, rel)
}

func waitForService(cfg config.Config) error {
	step("Waiting for the service")
	local := localService(cfg, "")
	for deadline := time.Now().Add(45 * time.Second); time.Now().Before(deadline); time.Sleep(time.Second) {
		if _, err := local.leader(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("the service didn't start; see %s", filepath.Join(config.StateDir(), "spinup.log"))
}

func step(format string, a ...any) { fmt.Printf("==> "+format+"\n", a...) }
