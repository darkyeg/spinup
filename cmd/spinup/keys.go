package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/tailnet"
)

// keys returns the API key for the launcher. The hub makes the keys; a standby copies them from
// the machine that holds the accounts; a machine that only uses them needs just the API key.
func (c installCmd) keys(cfg config.Config, ts tailnet.Status) (string, error) {
	if !cfg.Hold.CanHold() {
		return orAsk(c.APIKey, "API key (on the hub: spinup.py show-key): "), nil
	}
	if s, err := config.LoadSecrets(cfg); err == nil {
		step("Using the keys in %s", cfg.SecretsPath())
		return s.APIKey, nil
	}
	s, err := c.newSecrets(cfg, ts)
	if err != nil {
		return "", err
	}
	return s.APIKey, config.SaveSecrets(cfg, s)
}

func (c installCmd) newSecrets(cfg config.Config, ts tailnet.Status) (config.Secrets, error) {
	if cfg.Hold == config.HoldHub {
		step("Making new keys in %s", cfg.SecretsPath())
		return config.NewSecrets(), nil
	}
	password := orAsk(c.Password, "Dashboard password (on the hub: spinup.py show-key): ")
	s, err := fetchSecrets(ts, cfg.Port, password)
	if err == nil {
		step("Copied the keys from the machine that holds the accounts")
	}
	return s, err
}

// fetchSecrets asks the machine that holds the accounts, which checks the dashboard password.
func fetchSecrets(ts tailnet.Status, port int, password string) (config.Secrets, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	public := api.Client{HTTP: http.DefaultClient}
	keyed := api.Client{HTTP: http.DefaultClient, Key: password}
	for _, p := range ts.Peers {
		if !p.Online {
			continue
		}
		base := "http://" + p.IP + ":" + strconv.Itoa(port)
		var l api.Leader
		if public.Get(ctx, base+api.PathLeader, &l) != nil || !l.Leading {
			continue
		}
		var s config.Secrets
		if err := keyed.Get(ctx, base+api.PathSecrets, &s); err != nil {
			return s, fmt.Errorf("%s refused (wrong password?): %w", p.Name, err)
		}
		return s, nil
	}
	return config.Secrets{}, errors.New("no machine on your tailnet holds the accounts with spinup: install the hub first")
}

func orAsk(given, prompt string) string {
	if given != "" {
		return given
	}
	return askSecret(prompt)
}
