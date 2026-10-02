package spinup

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// divider matches a banner comment that splits a file into sections; a file that needs sections
// is split into files instead.
var divider = regexp.MustCompile(`^\s*(//|#)\s*[-=─*#]{4,}`)

var sourceExt = map[string]bool{".go": true, ".py": true, ".ps1": true, ".sh": true, ".yml": true, ".toml": true}

func TestTheDividerCheckCatchesBanners(t *testing.T) {
	for _, line := range []string{"// ---------------- daemon", "# ==== helpers", "\t// ****"} {
		if !divider.MatchString(line) {
			t.Errorf("%q is a divider", line)
		}
	}
	for _, line := range []string{"// a comment - with dashes", "#!/bin/sh", "x := a--"} {
		if divider.MatchString(line) {
			t.Errorf("%q is not a divider", line)
		}
	}
}

func TestNoDividerComments(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (strings.HasPrefix(d.Name(), ".") && path != "." && d.Name() != ".github" || d.Name() == "local") {
			return filepath.SkipDir
		}
		if d.IsDir() || !sourceExt[filepath.Ext(path)] {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		lines := bufio.NewScanner(f)
		for n := 1; lines.Scan(); n++ {
			if divider.MatchString(lines.Text()) {
				t.Errorf("%s:%d: divider comment; split the file instead", path, n)
			}
		}
		return lines.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
}
