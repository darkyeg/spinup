package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/tailnet"
)

// keys returns the API key for the launcher: the hub makes the keys, a standby copies them from the leader.
func (c installCmd) keys(cfg config.Config, ts tailnet.Status) (string, error) {
	if !cfg.Hold.CanHold() {
		return orAsk(c.APIKey, "API key (on the hub: spinup.py show-key): ", "API key")
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
	password, err := orAsk(c.Password, "Dashboard password (on the hub: spinup.py show-key): ", "dashboard password")
	if err != nil {
		return config.Secrets{}, err
	}
	s, err := fetchSecrets(ts, cfg.Port, password)
	if err == nil {
		step("Copied the keys from the machine that holds the accounts")
	}
	return s, err
}

// holderCandidate is an online device that may be the machine holding the accounts.
type holderCandidate struct{ name, base string }

func fetchSecrets(ts tailnet.Status, port int, password string) (config.Secrets, error) {
	var candidates []holderCandidate
	for _, p := range ts.Peers {
		if p.Online {
			candidates = append(candidates, holderCandidate{p.Name, "http://" + net.JoinHostPort(p.IP, strconv.Itoa(port))})
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return secretsFrom(ctx, candidates, password)
}

// secretsFrom sends the password only to a device that proves it knows it.
func secretsFrom(ctx context.Context, candidates []holderCandidate, password string) (config.Secrets, error) {
	public := api.Client{HTTP: http.DefaultClient}
	keyed := api.Client{HTTP: http.DefaultClient, Key: password}
	claimedWithoutProof := ""
	for _, c := range candidates {
		l, proof, err := public.AskLeader(ctx, c.base, c.name, config.Secrets{ManagementPassword: password})
		switch {
		case err != nil || !l.Leading || l.Name != c.name:
			continue
		case proof != api.KnowsPassword:
			claimedWithoutProof = c.name
			continue
		}
		var s config.Secrets
		if err := keyed.Get(ctx, c.base+api.PathSecrets, &s); err != nil {
			return s, fmt.Errorf("%s refused: %w", c.name, err)
		}
		return s, nil
	}
	if claimedWithoutProof != "" {
		return config.Secrets{}, fmt.Errorf("%s holds the accounts but doesn't know that password: wrong password?", claimedWithoutProof)
	}
	return config.Secrets{}, errors.New("no machine on your tailnet holds the accounts with spinup: install the hub first")
}

func orAsk(given, prompt, what string) (string, error) {
	if given == "" {
		given = askSecret(prompt)
	}
	if given == "" {
		return "", fmt.Errorf("no %s given", what)
	}
	return given, nil
}
