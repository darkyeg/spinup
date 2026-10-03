package skills

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"

	"github.com/darkyeg/spinup/internal/atomicfile"
	"github.com/darkyeg/spinup/internal/host"
	"github.com/darkyeg/spinup/internal/library"
	"github.com/darkyeg/spinup/internal/shell"
)

const (
	skillFile        = "SKILL.md"
	claudeSkillsName = "skills"
)

type paths struct {
	store  string
	parked string
	claude string
}

func hostPaths() paths {
	return paths{store: host.SkillsDir(), parked: host.ParkedSkillsDir(), claude: host.ClaudeHome()}
}

func (p paths) claudeSkills() string { return filepath.Join(p.claude, claudeSkillsName) }

type ownSkill struct {
	name  string
	files fs.FS
}

// findOwn lists the skill folders in your library, sorted by name.
func findOwn(lib fs.FS) ([]ownSkill, error) {
	entries, err := fs.ReadDir(lib, library.SkillsDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var found []ownSkill
	for _, entry := range entries {
		dir := path.Join(library.SkillsDir, entry.Name())
		if _, err := fs.Stat(lib, path.Join(dir, skillFile)); err != nil {
			continue
		}
		files, err := fs.Sub(lib, dir)
		if err != nil {
			return nil, err
		}
		found = append(found, ownSkill{entry.Name(), files})
	}
	return found, nil
}

// publish copies a skill into the store and links it into Claude Code's skills.
func (p paths) publish(ctx context.Context, skill ownSkill) error {
	for _, dir := range []string{p.store, p.claudeSkills()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	dest := filepath.Join(p.store, skill.name)
	if err := removeAny(dest); err != nil {
		return err
	}
	if err := os.CopyFS(dest, skill.files); err != nil {
		return err
	}
	return p.linkDir(ctx, filepath.Join(p.claudeSkills(), skill.name), dest)
}

func (p paths) linkDir(ctx context.Context, link, target string) error {
	if info, err := os.Lstat(link); err == nil && !isLink(link) && info.IsDir() {
		if err := p.parkAside(link, filepath.Base(link)); err != nil {
			return err
		}
	}
	if err := removeAny(link); err != nil {
		return err
	}
	if host.Windows {
		_, err := shell.Output(ctx, "cmd", "/c", "mklink", "/J", link, target)
		return err
	}
	return os.Symlink(target, link)
}

func removeAny(target string) error {
	if _, err := os.Lstat(target); err != nil {
		return nil
	}
	if isLink(target) {
		return os.Remove(target)
	}
	return os.RemoveAll(target)
}

func isLink(target string) bool {
	info, err := os.Lstat(target)
	return err == nil && info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0
}

func (p paths) removeDanglingLinks() error {
	entries, err := os.ReadDir(p.claudeSkills())
	if err != nil {
		return err
	}
	for _, entry := range entries {
		link := filepath.Join(p.claudeSkills(), entry.Name())
		if _, err := os.Stat(link); isLink(link) && err != nil {
			if err := os.Remove(link); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p paths) installed() ([]string, error) {
	entries, err := os.ReadDir(p.store)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if info, err := os.Stat(filepath.Join(p.store, entry.Name())); err == nil && info.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

func parkable(installed, keep []string, failedInstalls int) []string {
	if failedInstalls > 0 {
		return nil
	}
	var unlisted []string
	for _, name := range installed {
		if !slices.Contains(keep, name) {
			unlisted = append(unlisted, name)
		}
	}
	slices.Sort(unlisted)
	return unlisted
}

func (p paths) park(names []string) error {
	for _, name := range names {
		if err := os.MkdirAll(p.parked, 0o755); err != nil {
			return err
		}
		target := filepath.Join(p.parked, name)
		if err := os.RemoveAll(target); err != nil {
			return err
		}
		if err := atomicfile.Rename(filepath.Join(p.store, name), target); err != nil {
			return err
		}
	}
	return nil
}

func (p paths) parkAside(dir, name string) error {
	if err := os.MkdirAll(p.parked, 0o755); err != nil {
		return err
	}
	target := filepath.Join(p.parked, name)
	for n := 1; taken(target); n++ {
		target = filepath.Join(p.parked, fmt.Sprintf("%s-%d", name, n))
	}
	return atomicfile.Rename(dir, target)
}

func taken(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
