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
	"github.com/darkyeg/spinup/internal/source"
	"github.com/darkyeg/spinup/internal/tailnet"
)

// serviceRequest is what setup asks of the accounts service; an empty hold keeps the machine's hold.
type serviceRequest struct {
	hold             config.Hold
	password, apiKey string
	repo             source.Source
}

// installService runs the accounts service now and at every boot, and writes ccp.
func installService(ctx context.Context, req serviceRequest) error {
	cfg, err := config.Load()
	if err != nil && !errors.Is(err, config.ErrNotInstalled) {
		return err
	}
	if req.hold != "" || errors.Is(err, config.ErrNotInstalled) {
		cfg.Hold = cmp.Or(req.hold, config.HoldNever)
	}
	if cfg.Tailscale == "" {
		cfg.Tailscale = tailnet.Find()
	}
	if dir, err := req.repo.Checkout(); err == nil {
		cfg.Repo = dir
	}
	ts, err := tailnet.CLI{Bin: cfg.Tailscale}.Status(ctx)
	if err := tailscaleProblem(ts, err); err != nil {
		return err
	}
	step("This machine %s", holdPhrase(cfg.Hold))

	apiKey, err := machineKeys(cfg, ts, req)
	if err != nil {
		return err
	}
	if err := checkLauncherKey(apiKey); err != nil {
		return err
	}
	if err := ensureProxy(ctx, cfg); err != nil {
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
	step("Starting the accounts service at boot")
	if err := registerAutostart(exe, cfg); err != nil {
		return err
	}
	if err := writeLauncher(cfg.Port, apiKey); err != nil {
		return err
	}
	return waitForService(cfg)
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

func ensureProxy(ctx context.Context, cfg config.Config) error {
	if !cfg.Hold.CanHold() || proxy.Installed(cfg) {
		return nil
	}
	rel, err := release.Latest(ctx, proxy.Project)
	if err != nil {
		return fmt.Errorf("find the latest CLIProxyAPI: %w", err)
	}
	step("Installing CLIProxyAPI %s (checksum-verified)", rel.Version)
	return proxy.Install(ctx, cfg, rel)
}

func waitForService(cfg config.Config) error {
	local := localService(cfg, "")
	for deadline := time.Now().Add(45 * time.Second); time.Now().Before(deadline); time.Sleep(time.Second) {
		if _, err := local.leader(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("the service didn't start; see %s", filepath.Join(config.StateDir(), "spinup.log"))
}

func step(format string, a ...any) { fmt.Printf("==> "+format+"\n", a...) }
