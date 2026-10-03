package main

import (
	"fmt"
	"os"
)

func step(format string, a ...any) { fmt.Printf("==> "+format+"\n", a...) }

func orNone(s, none string) string {
	if s == "" {
		return none
	}
	return s
}

// interactive reports that a person is at the keyboard, so asking questions is welcome. A script, a pipe
// or the service must never be left waiting for an answer nobody can see.
func interactive() bool { return isTerminal(os.Stdin) && isTerminal(os.Stdout) }

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
