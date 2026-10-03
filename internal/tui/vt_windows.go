package tui

import (
	"os"

	"golang.org/x/sys/windows"
)

const utf8CodePage = 65001

// prepareOutput lets the console draw escape sequences and UTF-8 text, and returns how to put it back.
func prepareOutput(out *os.File) (restore func()) {
	handle := windows.Handle(out.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return func() {}
	}
	_ = windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
	codePage, err := windows.GetConsoleOutputCP()
	if err == nil {
		_ = windows.SetConsoleOutputCP(utf8CodePage)
	}
	return func() {
		_ = windows.SetConsoleMode(handle, mode)
		if err == nil {
			_ = windows.SetConsoleOutputCP(codePage)
		}
	}
}
