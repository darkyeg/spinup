//go:build !windows

// Package sysproc starts helper processes without a console window.
package sysproc

import "os/exec"

// Hide is a no-op outside Windows.
func Hide(*exec.Cmd) {}
