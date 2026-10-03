//go:build !windows

package tui

import "os"

// prepareOutput has nothing to do: other terminals already draw escape sequences and UTF-8.
func prepareOutput(*os.File) func() { return func() {} }
