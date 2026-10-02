package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/config"
)

const stopWithin = time.Minute

// local is this machine's spinup service, reached on localhost.
type local struct {
	base   string
	client api.Client
}

func localService(cfg config.Config, key string) local {
	return local{
		base:   fmt.Sprintf("http://127.0.0.1:%d", cfg.Port),
		client: api.Client{HTTP: &http.Client{Transport: &http.Transport{Proxy: nil}}, Key: key},
	}
}

// keyedLocalService is for commands that change who holds the accounts: only a machine that can hold has the key.
func keyedLocalService() (local, error) {
	cfg, err := config.Load()
	if err != nil {
		return local{}, err
	}
	if !cfg.Hold.CanHold() {
		return local{}, errors.New("this machine never holds the accounts; run this on the hub or a standby")
	}
	s, err := config.LoadSecrets(cfg)
	if err != nil {
		return local{}, err
	}
	return localService(cfg, s.ManagementPassword), nil
}

func (l local) leader() (api.Leader, error) {
	var out api.Leader
	return out, l.call(5*time.Second, http.MethodGet, api.PathLeader, nil, &out)
}

func (l local) report() (api.Report, error) {
	var out api.Report
	return out, l.call(10*time.Second, http.MethodGet, api.PathState, nil, &out)
}

func (l local) handOff(to string) error {
	return l.call(3*time.Minute, http.MethodPost, api.PathHandoff, api.Handoff{To: to}, nil)
}

func (l local) takeOver() error {
	return l.call(10*time.Second, http.MethodPost, api.PathTakeover, struct{}{}, nil)
}

// stop asks the service to hand the accounts on if it holds them and exit, then waits for it to be gone.
func (l local) stop() error {
	if err := l.call(10*time.Second, http.MethodPost, api.PathStop, struct{}{}, nil); err != nil {
		return err
	}
	for deadline := time.Now().Add(stopWithin); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if _, err := l.leader(); err != nil {
			return nil
		}
	}
	return errors.New("the service is still running")
}

func (l local) call(timeout time.Duration, method, path string, in, out any) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var err error
	if method == http.MethodGet {
		err = l.client.Get(ctx, l.base+path, out)
	} else {
		err = l.client.Post(ctx, l.base+path, in, out)
	}
	if unreachable := (*url.Error)(nil); errors.As(err, &unreachable) {
		return fmt.Errorf("spinup isn't answering on this machine (%v); is it installed? (spinup install)", unreachable.Err)
	}
	return err
}
