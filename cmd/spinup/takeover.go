package main

import (
	"errors"
	"fmt"
)

type takeoverCmd struct {
	Yes bool `short:"y" help:"Don't ask for confirmation."`
}

func (c takeoverCmd) Help() string {
	return `Use this only when the machine that held the accounts is gone for good (lost, broken disk).
If it comes back while this machine holds them, the two copies of the logins can log the
accounts out. In every other case spinup moves the accounts by itself.`
}

func (c takeoverCmd) Run() error {
	service, err := keyedLocalService()
	if err != nil {
		return err
	}
	if !c.Yes && !confirm("Is the machine that held the accounts gone for good?") {
		return errors.New("nothing changed")
	}
	if err := service.takeOver(); err != nil {
		return err
	}
	fmt.Println("This machine takes the accounts within a few seconds, unless another machine holds them.")
	return nil
}
