//go:build !windows && !linux && !darwin

package main

import (
	"errors"
	"runtime"

	"github.com/darkyeg/spinup/internal/config"
)

var errNoAutostart = errors.New("spinup can't start itself at boot on " + runtime.GOOS +
	" yet: run `spinup daemon` from your init system")

func registerAutostart(string, config.Config) error { return errNoAutostart }

func unregisterAutostart(config.Config) error { return errNoAutostart }

func autostartRegistered() bool { return false }
