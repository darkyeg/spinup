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

func interactive() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
