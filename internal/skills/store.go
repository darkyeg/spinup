package skills

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/darkyeg/spinup/internal/host"
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

func findOwn(shared, private fs.FS) ([]ownSkill, error) {
	byName := map[string]ownSkill{}
	for _, base := range []struct {
		root fs.FS
		dir  string
	}{{shared, "skills/local"}, {private, "skills"}} {
		if base.root == nil {
			continue
		}
		entries, err := fs.ReadDir(base.root, base.dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			dir := path.Join(base.dir, entry.Name())
			if _, err := fs.Stat(base.root, path.Join(dir, skillFile)); err != nil {
				continue
			}
			files, err := fs.Sub(base.root, dir)
			if err != nil {
				return nil, err
			}
			byName[entry.Name()] = ownSkill{entry.Name(), files}
		}
	}
	var found []ownSkill
	for _, skill := range byName {
		found = append(found, skill)
	}
	slices.SortFunc(found, func(a, b ownSkill) int { return strings.Compare(a.name, b.name) })
	return found, nil
}

func (p paths) publish(ctx context.Context, skill ownSkill) error {
	dest := filepath.Join(p.store, skill.name)
	if err := removeAny(dest); err != nil {
		return err
	}
	if err := os.CopyFS(dest, skill.files); err != nil {
		return err
	}
	return linkDir(ctx, filepath.Join(p.claudeSkills(), skill.name), dest)
}

func linkDir(ctx context.Context, link, target string) error {
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
		if err := os.Rename(filepath.Join(p.store, name), target); err != nil {
			return err
		}
	}
	return nil
}
