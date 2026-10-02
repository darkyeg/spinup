//go:build !windows

package main

import "github.com/darkyeg/spinup/internal/host"

// ensureOnPath tells the user how to put dir on PATH when it isn't; shell profiles are the user's to edit.
func ensureOnPath(dir string) error {
	if !host.OnPath(dir) {
		step("%s is not on your PATH, so `ccp` won't be found. Add this line to your shell profile (~/.zshrc or ~/.bashrc), then open a new terminal:\n"+
			"    export PATH=\"%s:$PATH\"", dir, dir)
	}
	return nil
}
