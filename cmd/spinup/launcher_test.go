package main

import "testing"

func TestLauncherKeysCannotBreakTheScripts(t *testing.T) {
	cases := map[string]bool{
		"sk-0123abcDEF":      true,
		"a.b_c~d+e/f=g-h":    true,
		"":                   false,
		`quote"`:             false,
		"dollar$HOME":        false,
		"percent%PATH%":      false,
		"space in key":       false,
		"line\nbreak":        false,
		"back`tick":          false,
		"caret^and&ampersnd": false,
	}
	for key, ok := range cases {
		if err := checkLauncherKey(key); (err == nil) != ok {
			t.Errorf("key %q: err = %v, want ok = %v", key, err, ok)
		}
	}
}
