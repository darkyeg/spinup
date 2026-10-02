// Package source finds spinup's own data (the suggested skills, the instructions, the tools): a checkout
// of the spinup repo when you work on spinup itself, or else the copy built into the binary. What is
// yours lives in your library instead (internal/library).
package source

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/darkyeg/spinup"
)

var ErrNoCheckout = errors.New("no spinup checkout here: spinup uses its built-in data")

type Source struct {
	data     fs.FS
	checkout string
}

// Find looks in SPINUP_REPO, then saved, then the folders above the working directory and above
// this binary; without a checkout it uses the built-in copy.
func Find(saved string) Source {
	for _, dir := range candidates(saved) {
		if root, ok := checkoutAbove(dir); ok {
			return Source{data: os.DirFS(root), checkout: root}
		}
	}
	return Source{data: spinup.Files()}
}

// Builtin is the data built into this binary.
func Builtin() Source { return Source{data: spinup.Files()} }

// At uses the checkout at root; tests use it with a temporary folder.
func At(root string) Source { return Source{data: os.DirFS(root), checkout: root} }

// Data is the repo's files: agents/, tools.json, skills/*.json.
func (s Source) Data() fs.FS { return s.data }

// Checkout is the repo folder, when spinup runs from one.
func (s Source) Checkout() (string, error) {
	if s.checkout == "" {
		return "", ErrNoCheckout
	}
	return s.checkout, nil
}

func candidates(saved string) []string {
	dirs := []string{os.Getenv("SPINUP_REPO"), saved}
	if wd, err := os.Getwd(); err == nil {
		dirs = append(dirs, wd)
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	return dirs
}

func checkoutAbove(dir string) (string, bool) {
	if dir == "" {
		return "", false
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for {
		if isCheckout(dir) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func isCheckout(dir string) bool {
	for _, marker := range []string{"tools.json", filepath.Join("agents", "AGENTS.md"), filepath.Join("skills", "skills.json")} {
		if _, err := os.Stat(filepath.Join(dir, marker)); err != nil {
			return false
		}
	}
	return true
}
