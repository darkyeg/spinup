package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/darkyeg/spinup/internal/agentconfig"
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/library"
	"github.com/darkyeg/spinup/internal/proxy"
	"github.com/darkyeg/spinup/internal/service"
	"github.com/darkyeg/spinup/internal/skills"
	"github.com/darkyeg/spinup/internal/source"
	"github.com/darkyeg/spinup/internal/tailnet"
)

type daemonCmd struct {
	Home string `help:"The state folder; boot tasks may start without the usual environment."`
}

func (c daemonCmd) Run() error {
	if c.Home != "" {
		os.Setenv("SPINUP_HOME", c.Home)
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

	secrets, err := config.LoadSecrets(cfg)
	if err != nil {
		return fmt.Errorf("%w (run spinup setup again)", err)
	}
	restoreHome(cfg.Home)
	o := service.Options{Config: cfg, Secrets: secrets, Tailnet: tailnet.CLI{Bin: cfg.Tailscale}, Log: logger, Version: version}
	shareLibrary(&o, cfg, logger)
	if cfg.Hold.CanHold() {
		if err := withProxy(&o, cfg, logger); err != nil {
			return err
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := service.New(o).Run(ctx); err != nil {
		logger.Printf("stopped: %v", err)
		return err
	}
	return nil
}

// shareLibrary shares your library with your other machines and installs one that arrives from them.
func shareLibrary(o *service.Options, cfg config.Config, logger *log.Logger) {
	repo := source.Find(cfg.Repo)
	lib := library.Here()
	if cfg.Library != "" {
		lib = library.At(cfg.Library)
	}
	o.Library = lib
	o.ApplyLibrary = func(ctx context.Context) error {
		_, skillsErr := skills.New(repo, lib, logger.Printf).Offline().Sync(ctx)
		_, agentsErr := agentconfig.Install(agentconfig.Sources{Spinup: repo, Yours: lib}, agentconfig.HostHomes())
		return errors.Join(skillsErr, agentsErr)
	}
}

// restoreHome gives a service started at boot without a home folder the one setup saw.
func restoreHome(home string) {
	if _, err := os.UserHomeDir(); err == nil || home == "" {
		return
	}
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)
}

func withProxy(o *service.Options, cfg config.Config, logger *log.Logger) error {
	if !proxy.Installed(cfg) {
		return fmt.Errorf("CLIProxyAPI isn't in %s (run spinup setup again)", cfg.ProxyDir)
	}
	o.Proxy = &proxy.Runner{Exe: cfg.ProxyExe(), Dir: cfg.ProxyDir, Config: cfg.ProxyConfig(), Port: cfg.ProxyPort, Log: logger}
	o.PrepareProxy = func() error { return proxy.WriteConfig(cfg, o.Secrets) }
	return nil
}

// openLog appends to spinup.log in the state folder, starting a new file past 5 MB.
func openLog() (*log.Logger, func(), error) {
	path := filepath.Join(config.StateDir(), "spinup.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, err
	}
	if fi, err := os.Stat(path); err == nil && fi.Size() > 5<<20 {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, err
	}
	return log.New(io.MultiWriter(f, os.Stderr), "", log.LstdFlags), func() { f.Close() }, nil
}
