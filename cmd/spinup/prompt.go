package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"

	"golang.org/x/term"
)

// stdin is read through one buffered reader for the whole program. A second reader over os.Stdin
// would start by filling its own buffer, swallowing the lines a later prompt is waiting for.
var stdin = sync.OnceValue(func() *bufio.Reader { return bufio.NewReader(os.Stdin) })

// askSecret reads a line without echoing it when stdin is a terminal.
func askSecret(prompt string) string {
	fmt.Print(prompt)
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return readLine()
	}
	secret, _ := term.ReadPassword(fd)
	fmt.Println()
	return strings.TrimSpace(string(secret))
}

// confirm asks a yes-or-no question. Anything but yes, including no answer at all, means no.
func confirm(prompt string) bool {
	fmt.Print(prompt + " [y/N] ")
	answer := strings.ToLower(readLine())
	return answer == "y" || answer == "yes"
}

// askLine reads one line of input.
func askLine(prompt string) string {
	fmt.Print(prompt)
	return readLine()
}

func readLine() string {
	line, _ := stdin().ReadString('\n')
	return strings.TrimSpace(line)
}
