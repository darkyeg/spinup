package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/proxy"
	"github.com/darkyeg/spinup/internal/release"
	"github.com/darkyeg/spinup/internal/source"
	"github.com/darkyeg/spinup/internal/tailnet"
)

const (
	serviceStartWithin = 45 * time.Second
	leaderReachWithin  = 20 * time.Second
	pollEvery          = time.Second
)

// serviceRequest is what setup asks of the accounts service; an empty hold keeps the machine's hold.
type serviceRequest struct {
	hold             config.Hold
	password, apiKey string
	repo             source.Source
}

// installService runs the accounts service now and at every boot, and writes ccp.
func installService(ctx context.Context, req serviceRequest) error {
	before := storedSettings()
	cfg, err := serviceConfig(req)
	if err != nil {
		return err
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
	if err := startAtBoot(cfg, before != storedSettings()); err != nil {
		return err
	}
	if err := writeLauncher(cfg.Port, apiKey); err != nil {
		return err
	}
	if err := waitForService(cfg); err != nil {
		return err
	}
	reportAccess(cfg)
	return nil
}

func serviceConfig(req serviceRequest) (config.Config, error) {
	cfg, err := config.Load()
	if err != nil && !errors.Is(err, config.ErrNotInstalled) {
		return cfg, err
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
	return cfg, nil
}

func startAtBoot(cfg config.Config, settingsChanged bool) error {
	reason := restartReason(currentFacts(cfg, settingsChanged))
	if reason == "" {
		step("The accounts service is running and unchanged; leaving it alone")
		return nil
	}
	exe, err := stageBinary()
	if err != nil {
		return err
	}
	stopService()
	step("Starting the accounts service at boot (%s)", reason)
	return registerAutostart(exe, cfg)
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
	started := pollUntil(serviceStartWithin, func() bool {
		_, err := local.leader()
		return err == nil
	})
	if !started {
		return fmt.Errorf("the service didn't start; see %s", filepath.Join(config.StateDir(), "spinup.log"))
	}
	return nil
}

func reportAccess(cfg config.Config) {
	local := localService(cfg, "")
	if cfg.Hold.CanHold() {
		reportDashboard(cfg, local)
		return
	}
	reached := pollUntil(leaderReachWithin, func() bool {
		r, err := local.report()
		return err == nil && reachesLeader(r)
	})
	if !reached {
		step("Warning: this machine doesn't reach the accounts yet. The hub or standby may be off, or the API key may be wrong. " +
			"Check with `spinup status`; to fix the key run `spinup setup <name> --api-key <key>` (`spinup keys` on the hub).")
	}
}

func reachesLeader(r api.Report) bool { return r.Leader != "" && r.LeaderAddr != "" }

func reportDashboard(cfg config.Config, local local) {
	step("Dashboard: %s", dashboardURL(cfg.Port))
	if cfg.Hold != config.HoldHub {
		return
	}
	var report api.Report
	leading := pollUntil(leaderReachWithin, func() bool {
		r, err := local.report()
		report = r
		return err == nil && r.Leading
	})
	if leading && len(report.Accounts) == 0 {
		step("No accounts yet: open the dashboard > OAuth Login to add them")
	}
}

func dashboardURL(port int) string { return fmt.Sprintf("http://localhost:%d/management.html", port) }

func pollUntil(within time.Duration, done func() bool) bool {
	for deadline := time.Now().Add(within); ; time.Sleep(pollEvery) {
		if done() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
	}
}
