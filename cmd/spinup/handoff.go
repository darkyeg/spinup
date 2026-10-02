package main

import "fmt"

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
	fmt.Printf("Moving the accounts to %s...\n", c.Machine)
	if err := service.handOff(c.Machine); err != nil {
		return err
	}
	fmt.Printf("%s holds the accounts.\n", c.Machine)
	return nil
}
