package main

import (
	"fmt"

	"github.com/darkyeg/spinup/internal/config"
)

type keysCmd struct{}

func (keysCmd) Help() string {
	return `Prints the API key, which a machine that only uses the accounts asks for during setup, and
the dashboard password, which a standby asks for. Every machine keeps the API key; only a hub or
standby keeps the dashboard password.`
}

func (keysCmd) Run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	s, err := config.LoadSecrets(cfg)
	if err != nil {
		return err
	}
	if !cfg.Hold.CanHold() {
		fmt.Printf("API key:  %s\n\nThe dashboard password is kept only on the hub and standbys: run `spinup keys` there.\n", s.APIKey)
		return nil
	}
	fmt.Printf("API key:             %s\nDashboard password:  %s\n", s.APIKey, s.ManagementPassword)
	return nil
}
