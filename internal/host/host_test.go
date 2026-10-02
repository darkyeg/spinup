package host

import (
	"path/filepath"
	"testing"
)

func TestPathWithAddsADirectoryOnlyOnce(t *testing.T) {
	bin := filepath.Join("home", "me", ".local", "bin")
	other := filepath.Join("usr", "bin")
	sep := string(filepath.ListSeparator)

	if got, added := PathWith("", bin); got != bin || !added {
		t.Errorf("empty list: %q, %v", got, added)
	}
	if got, added := PathWith(other, bin); got != other+sep+bin || !added {
		t.Errorf("other dirs: %q, %v", got, added)
	}
	if got, added := PathWith(other+sep+bin, bin); got != other+sep+bin || added {
		t.Errorf("already there: %q, %v", got, added)
	}
}
