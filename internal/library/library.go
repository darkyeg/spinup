// Package library is what is yours: your skills list, your own skills and your instructions, kept in
// one folder on each machine (~/.spinup) and shared between your machines. There is no repo to clone or pull.
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
	// Fetched keeps a copy of each skill on your list that came from GitHub, so no machine fetches it again.
	Fetched = "fetched"
)

// yourParts are what you make in a library; they date it.
var yourParts = []string{SkillsList, SkillsDir, Instructions}

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

// Adopt copies the private repo of earlier spinup versions (old) into the library, once: only into a
// library with nothing of yours yet. It returns what it copied; old stays as it was.
func (l Library) Adopt(old string) (adopted []string, err error) {
	for _, name := range yourParts {
		if _, err := os.Stat(l.Path(name)); err == nil {
			return nil, nil
		}
	}
	for _, name := range yourParts {
		from := filepath.Join(old, name)
		info, err := os.Stat(from)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return adopted, err
		}
		if err := copyEntry(from, l.Path(name), info); err != nil {
			return adopted, err
		}
		adopted = append(adopted, name)
	}
	return adopted, nil
}

// copyEntry copies a file or folder and keeps each file's modification time, which dates the library.
func copyEntry(from, to string, info fs.FileInfo) error {
	if !info.IsDir() {
		return copyFile(from, to, info)
	}
	return filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(from, p)
		if err != nil {
			return err
		}
		fileInfo, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(p, filepath.Join(to, rel), fileInfo)
	})
}

func copyFile(from, to string, info fs.FileInfo) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(to, data, 0o644); err != nil {
		return err
	}
	return os.Chtimes(to, info.ModTime(), info.ModTime())
}
