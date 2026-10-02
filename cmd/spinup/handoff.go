package main

import (
	"fmt"
	"strings"
)

type handoffCmd struct {
	Machine string `arg:"" help:"The hub or standby that takes the accounts."`
}

func (c handoffCmd) Help() string {
	return `The machine holding the accounts stops its proxy, sends its final logins, and the target starts
only once it has them. Run it on any hub or standby.`
}

func (c handoffCmd) Run() error {
	service, err := keyedLocalService()
	if err != nil {
		return err
	}
	target := strings.ToLower(c.Machine)
	fmt.Printf("Moving the accounts to %s...\n", target)
	if err := service.handOff(target); err != nil {
		return err
	}
	fmt.Printf("%s holds the accounts.\n", target)
	return nil
}
