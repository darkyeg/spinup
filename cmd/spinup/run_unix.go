//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

// run shares this terminal, so the command can show output and ask for a password.
func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func runAsRoot(name string, args ...string) error {
	if os.Geteuid() == 0 {
		return run(name, args...)
	}
	return run("sudo", append([]string{name}, args...)...)
}

func currentUser() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return os.Getenv("USER")
}
