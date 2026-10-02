// Command spinup runs the spinup service and talks to it.
//
//	spinup install --role hub|standby|client   set up the service on this machine
//	spinup status                              who holds the accounts, sync state, every machine
//	spinup handoff <machine>                   move the accounts to another machine, safely
//	spinup lead --force                        take the accounts when the other machine is lost
//	spinup uninstall                           remove the service from this machine
//	spinup daemon                              run the service (started for you at boot)
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/darkyeg/spinup/internal/cluster"
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/proxy"
	"github.com/darkyeg/spinup/internal/tailnet"
)

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

const usage = `spinup: your AI accounts on all your machines, never logged out.

Usage:
  spinup install --role hub|standby|client [--key KEY]
      hub      the machine that normally holds the accounts (one per tailnet)
      standby  keeps a synced copy and takes over when the hub is off
      client   uses the accounts through whichever machine holds them
  spinup status               who holds the accounts, sync state, every machine
  spinup handoff <machine>    move the accounts to another machine, safely
  spinup lead --force         take the accounts here (only if the other machine is lost for good)
  spinup uninstall            remove the service from this machine
  spinup daemon               run the service in the foreground (installed to start by itself)
  spinup version

Every machine reaches the accounts at http://localhost:8317, whichever machine holds them.
Docs: https://github.com/darkyeg/spinup/blob/main/docs/SERVICE.md
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "daemon":
		err = cmdDaemon(args)
	case "install":
		err = cmdInstall(args)
	case "uninstall":
		err = cmdUninstall(args)
	case "status":
		err = cmdStatus(args)
	case "handoff":
		err = cmdHandoff(args)
	case "lead":
		err = cmdLead(args)
	case "version", "--version", "-v":
		fmt.Println("spinup", version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------- daemon

func cmdDaemon(args []string) error {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	home := fs.String("home", "", "state folder (default: per-user app data)")
	_ = fs.Parse(args)
	if *home != "" {
		os.Setenv("SPINUP_HOME", *home) // boot tasks may not have the usual environment
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger, closeLog, err := openLog()
	if err != nil {
		return err
	}
	defer closeLog()

	o := cluster.Options{Config: cfg, Tailnet: tailnet.CLI{Bin: cfg.Tailscale}, Log: logger, Version: version}
	if cfg.Role.Eligible() {
		if o.Secrets, err = config.LoadSecrets(cfg); err != nil {
			return fmt.Errorf("%w (run `spinup install` again)", err)
		}
		if !proxy.Installed(cfg) {
			return fmt.Errorf("CLIProxyAPI is not installed in %s (run `spinup install` again)", cfg.ProxyDir)
		}
		secrets := o.Secrets
		o.Runner = &proxy.Runner{Exe: cfg.ProxyExe(), Dir: cfg.ProxyDir, Config: cfg.ProxyConfig(), Port: cfg.ProxyPort, Log: logger}
		o.PrepareProxy = func() error { return proxy.WriteConfig(cfg, secrets) }
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = cluster.New(o).Run(ctx)
	if err != nil {
		logger.Printf("stopped: %v", err)
	}
	return err
}

func openLog() (*log.Logger, func(), error) {
	dir := config.StateDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, err
	}
	path := filepath.Join(dir, "spinup.log")
	if fi, err := os.Stat(path); err == nil && fi.Size() > 5<<20 {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, err
	}
	return log.New(io.MultiWriter(f, os.Stderr), "", log.LstdFlags), func() { f.Close() }, nil
}

// ---------------------------------------------------------------- talking to the service

func frontURL(cfg config.Config, path string) string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", cfg.Port, path)
}

func callService(method, url, key string, body, out any) error {
	var rd io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("X-Spinup-Key", key)
	}
	client := &http.Client{Timeout: 3 * time.Minute, Transport: &http.Transport{Proxy: nil}}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("the spinup service isn't answering (%v); is it installed and running?", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error.Message != "" {
			return errors.New(strings.TrimPrefix(e.Error.Message, "spinup: "))
		}
		return fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(data))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func cmdStatus(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	var r cluster.Report
	if err := callService(http.MethodGet, frontURL(cfg, "/spinup/state"), "", nil, &r); err != nil {
		return err
	}
	if len(args) > 0 && args[0] == "--json" {
		out, _ := json.MarshalIndent(r, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	fmt.Printf("This machine  %s (%s), spinup %s\n", r.Name, r.Role, r.Version)
	switch {
	case r.IsLeader:
		fmt.Printf("Accounts      held here (epoch %d); proxy %s\n", r.Epoch, map[bool]string{true: "running", false: "NOT running"}[r.ProxyRunning])
	case r.Role == config.RoleClient && r.LeaderAddr != "":
		fmt.Printf("Accounts      used through %s\n", r.LeaderAddr)
	case r.Leader != "" && r.Waiting == "":
		sync := "not synced yet"
		if r.SyncedSecondsAgo >= 0 {
			sync = fmt.Sprintf("synced %s ago", (time.Duration(r.SyncedSecondsAgo) * time.Second).String())
		}
		fmt.Printf("Accounts      held by %s (epoch %d); copy here %s\n", r.Leader, r.Epoch, sync)
	default:
		fmt.Printf("Accounts      nobody holds them right now\n")
	}
	if r.Waiting != "" {
		fmt.Printf("Waiting       %s\n", r.Waiting)
	}
	fmt.Printf("Address       http://localhost:%d (API, dashboard at /management.html)\n", cfg.Port)
	if len(r.Accounts) > 0 {
		fmt.Printf("\nLogins (%d)\n", len(r.Accounts))
		for _, a := range r.Accounts {
			when := "unknown"
			if !a.Refreshed.IsZero() {
				when = time.Since(a.Refreshed).Round(time.Minute).String() + " ago"
			}
			fmt.Printf("  %-8s refreshed %-14s %s\n", a.Type, when, a.Name)
		}
	}
	if len(r.Peers) > 0 {
		sort.Slice(r.Peers, func(i, j int) bool { return r.Peers[i].Name < r.Peers[j].Name })
		fmt.Printf("\nOther machines\n")
		for _, p := range r.Peers {
			state := "offline"
			switch {
			case p.Reachable && p.IsLeader:
				state = "holds the accounts"
			case p.Reachable && p.Synced:
				state = "standing by, synced"
			case p.Reachable:
				state = "standing by"
			case p.Online:
				state = "online, service not answering"
			default:
				state = "offline " + p.OfflineFor.Round(time.Second).String()
			}
			fmt.Printf("  %-16s %-8s %s\n", p.Name, p.Role, state)
		}
	}
	return nil
}

func cmdHandoff(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: spinup handoff <machine>")
	}
	cfg, sec, err := loadWithSecrets()
	if err != nil {
		return err
	}
	fmt.Printf("Moving the accounts to %s...\n", args[0])
	if err := callService(http.MethodPost, frontURL(cfg, "/spinup/handoff"), sec.ManagementPassword,
		map[string]string{"to": args[0]}, nil); err != nil {
		return err
	}
	fmt.Printf("Done: %s holds the accounts.\n", args[0])
	return nil
}

func cmdLead(args []string) error {
	if len(args) != 1 || args[0] != "--force" {
		return errors.New("usage: spinup lead --force\n\n" +
			"Only use this when the machine that may hold newer logins is lost for good: if it comes back\n" +
			"while this one holds the accounts, the two copies can log the accounts out.")
	}
	cfg, sec, err := loadWithSecrets()
	if err != nil {
		return err
	}
	if err := callService(http.MethodPost, frontURL(cfg, "/spinup/force-lead"), sec.ManagementPassword, nil, nil); err != nil {
		return err
	}
	fmt.Println("This machine takes the accounts within a few seconds (unless another machine holds them).")
	return nil
}

func loadWithSecrets() (config.Config, config.Secrets, error) {
	cfg, err := config.Load()
	if err != nil {
		return cfg, config.Secrets{}, err
	}
	if !cfg.Role.Eligible() {
		return cfg, config.Secrets{}, errors.New("run this on a hub or standby machine")
	}
	sec, err := config.LoadSecrets(cfg)
	return cfg, sec, err
}
