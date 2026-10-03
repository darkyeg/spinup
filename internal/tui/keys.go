// Package tui is a small full-screen list: tabs of rows you move through, mark and act on. The model
// and the drawing are pure, so they are tested without a terminal; terminal.go is the thin adapter.
package tui

// KeyKind is what a key press means, apart from the letter it may carry.
type KeyKind int

const (
	Rune KeyKind = iota
	Up
	Down
	Left
	Right
	PageUp
	PageDown
	Home
	End
	Enter
	TabKey
	Backspace
	Esc
	CtrlC
)

type Key struct {
	Kind KeyKind
	Rune rune
}

// Decode turns the bytes one read returned into key presses. A read can hold several, such as a paste.
func Decode(in []byte) []Key {
	var keys []Key
	for len(in) > 0 {
		key, used := decodeOne(in)
		keys = append(keys, key)
		in = in[used:]
	}
	return keys
}

var escapes = map[string]KeyKind{
	"[A": Up, "[B": Down, "[C": Right, "[D": Left, "[H": Home, "[F": End,
	"OA": Up, "OB": Down, "OC": Right, "OD": Left, "OH": Home, "OF": End,
	"[5~": PageUp, "[6~": PageDown, "[1~": Home, "[4~": End, "[7~": Home, "[8~": End,
}

func decodeOne(in []byte) (Key, int) {
	switch in[0] {
	case 0x03:
		return Key{Kind: CtrlC}, 1
	case '\r', '\n':
		return Key{Kind: Enter}, 1
	case '\t':
		return Key{Kind: TabKey}, 1
	case 0x7f, 0x08:
		return Key{Kind: Backspace}, 1
	case 0x1b:
		return decodeEscape(in)
	}
	for size := 1; size <= 4 && size <= len(in); size++ {
		if r := []rune(string(in[:size])); len(r) == 1 && r[0] != 0xFFFD {
			return Key{Kind: Rune, Rune: r[0]}, size
		}
	}
	return Key{Kind: Rune, Rune: rune(in[0])}, 1
}

// decodeEscape reads an arrow or page key; a lone Esc, or one that starts nothing known, is just Esc.
func decodeEscape(in []byte) (Key, int) {
	for size := 3; size <= 4 && size <= len(in); size++ {
		if kind, ok := escapes[string(in[1:size])]; ok {
			return Key{Kind: kind}, size
		}
	}
	return Key{Kind: Esc}, 1
}
