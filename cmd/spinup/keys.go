package main

import (
	"errors"
	"fmt"

	"github.com/darkyeg/spinup/internal/config"
)

type keysCmd struct{}

func (keysCmd) Help() string {
	return `Prints the API key and the dashboard password, which other machines ask for during setup.
Only a hub or standby has them.`
}

func (keysCmd) Run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if !cfg.Hold.CanHold() {
		return errors.New("this machine only uses the accounts and keeps no keys; run this on the hub or a standby")
	}
	s, err := config.LoadSecrets(cfg)
	if err != nil {
		return err
	}
	fmt.Printf("API key:             %s\nDashboard password:  %s\n", s.APIKey, s.ManagementPassword)
	return nil
}
