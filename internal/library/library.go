// Package library is what is yours: your skills list, your own skills and your instructions, kept in
// one folder on each machine (~/.spinup). There is no repo to clone or pull.
package library

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/darkyeg/spinup/internal/host"
)

const (
	// SkillsList is your skills list; until it exists, spinup's own list is used.
	SkillsList = "skills.json"
	// SkillsDir holds your own skills, one folder with a SKILL.md each.
	SkillsDir = "skills"
	// Instructions is added after spinup's instructions for Claude Code and Codex.
	Instructions = "AGENTS.md"
)

type Library struct{ dir string }

// Here is this user's library: SPINUP_LIBRARY, or ~/.spinup.
func Here() Library {
	if dir := os.Getenv("SPINUP_LIBRARY"); dir != "" {
		return At(dir)
	}
	return At(filepath.Join(host.Home(), ".spinup"))
}

func At(dir string) Library { return Library{dir: dir} }

func (l Library) Dir() string { return l.dir }

// Files reads the library; a library that doesn't exist yet reads as empty.
func (l Library) Files() fs.FS { return os.DirFS(l.dir) }

// Path is where name lives in the library.
func (l Library) Path(name string) string { return filepath.Join(l.dir, filepath.FromSlash(name)) }

// Adopt copies the private repo of earlier spinup versions (old) into the library, once: only while the
// library has none of what old has. It returns what it copied; old stays as it was.
func (l Library) Adopt(old string) (adopted []string, err error) {
	for _, name := range []string{SkillsList, SkillsDir, Instructions} {
		from := filepath.Join(old, name)
		info, err := os.Stat(from)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return adopted, err
		}
		if _, err := os.Stat(l.Path(name)); err == nil {
			continue
		}
		if err := copyEntry(from, l.Path(name), info); err != nil {
			return adopted, err
		}
		adopted = append(adopted, name)
	}
	return adopted, nil
}

func copyEntry(from, to string, info fs.FileInfo) error {
	if info.IsDir() {
		return os.CopyFS(to, os.DirFS(from))
	}
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	return os.WriteFile(to, data, 0o644)
}
