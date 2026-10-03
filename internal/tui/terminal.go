package tui

import (
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

const (
	enterScreen = "\x1b[?1049h\x1b[?25l\x1b[2J"
	leaveScreen = "\x1b[?25h\x1b[?1049l"
)

// Act carries out what the person asked for and returns the lists as they are afterwards, with a line
// saying what happened. An error is shown on the screen and the person carries on.
type Act func(Intent) (tabs []Tab, notice string, err error)

var ErrNotTerminal = errors.New("a full-screen list needs a terminal")

// Run shows the model until the person quits, calling act for every action they ask for.
func Run(model *Model, act Act) error {
	in, out := os.Stdin, os.Stdout
	if !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		return ErrNotTerminal
	}
	restoreOutput := prepareOutput(out)
	defer restoreOutput()
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return err
	}
	defer term.Restore(int(in.Fd()), state)
	fmt.Fprint(out, enterScreen)
	defer fmt.Fprint(out, leaveScreen)

	color := os.Getenv("NO_COLOR") == ""
	buffer := make([]byte, 64)
	for !model.Done() {
		if width, height, err := term.GetSize(int(out.Fd())); err == nil {
			model.SetSize(width, height)
		}
		fmt.Fprint(out, model.View(color))
		n, err := in.Read(buffer)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		for _, key := range Decode(buffer[:n]) {
			if intent, ok := model.Update(key); ok {
				apply(model, act, intent)
			}
		}
	}
	return nil
}

func apply(model *Model, act Act, intent Intent) {
	tabs, notice, err := act(intent)
	if err != nil {
		model.Notice = "Error: " + err.Error()
		return
	}
	model.Replace(tabs, notice)
}
