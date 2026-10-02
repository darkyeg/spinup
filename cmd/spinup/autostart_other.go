//go:build !windows && !linux && !darwin

package main

import (
	"errors"
	"runtime"

	"github.com/darkyeg/spinup/internal/config"
)

var errNoAutostart = errors.New("starting the service at boot isn't supported on " + runtime.GOOS +
	" yet: run `spinup daemon` from your init system")

func registerAutostart(string, config.Config) error { return errNoAutostart }

func unregisterAutostart(config.Config) error { return errNoAutostart }
