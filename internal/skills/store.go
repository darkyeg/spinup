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
	"strings"

	"github.com/darkyeg/spinup/internal/atomicfile"
	"github.com/darkyeg/spinup/internal/host"
	"github.com/darkyeg/spinup/internal/library"
)

const (
	skillFile       = "SKILL.md"
	agentSkillsName = "skills"
)

type paths struct {
	store  string
	parked string
	claude string
	codex  string
}

func hostPaths() paths {
	return paths{
		store:  host.SkillsDir(),
		parked: host.ParkedSkillsDir(),
		claude: host.ClaudeHome(),
		codex:  host.CodexHome(),
	}
}

func (p paths) claudeSkills() string { return filepath.Join(p.claude, agentSkillsName) }

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

// publish copies a skill into the store and links it into every agent's skills folder. The copy is
// staged and swapped in, so the store path never vanishes and the agents' links to it stay good.
func (p paths) publish(ctx context.Context, skill ownSkill) error {
	if err := os.MkdirAll(p.store, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(p.store, skill.name)
	staging, err := os.MkdirTemp(p.store, "."+skill.name+"-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	staged := filepath.Join(staging, skill.name)
	if err := os.CopyFS(staged, skill.files); err != nil {
		return err
	}
	if err := swapDir(staged, dest, p.store); err != nil {
		return err
	}
	return p.link(ctx, skill.name)
}

// swapDir puts staged at dest, moving any old copy aside first so dest is never missing for long.
func swapDir(staged, dest, within string) error {
	old, err := os.MkdirTemp(within, ".old-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(old)
	replacing := taken(dest)
	if replacing {
		if err := atomicfile.Rename(dest, filepath.Join(old, "it")); err != nil {
			return err
		}
	}
	if err := atomicfile.Rename(staged, dest); err != nil {
		if replacing {
			_ = atomicfile.Rename(filepath.Join(old, "it"), dest)
		}
		return err
	}
	return nil
}

// link points every agent's skills folder at the store's copy of name.
func (p paths) link(ctx context.Context, name string) error {
	dest := filepath.Join(p.store, name)
	for _, dir := range p.agentSkills() {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := p.linkDir(filepath.Join(dir, name), dest); err != nil {
			return err
		}
	}
	return nil
}

// linkAll repairs the links for every skill the agents should see, including the ones the store
// already had: a skill is fetched once, so without this only the machine that fetched it is linked.
func (p paths) linkAll(ctx context.Context, keep []string) error {
	for _, name := range keep {
		if !dirExists(filepath.Join(p.store, name)) {
			continue
		}
		if err := p.link(ctx, name); err != nil {
			return fmt.Errorf("linking %s: %w", name, err)
		}
	}
	return nil
}

func (p paths) linkDir(link, target string) error {
	if alreadyLinks(link, target) {
		return nil
	}
	if info, err := os.Lstat(link); err == nil && !isLink(link) && info.IsDir() {
		if err := p.parkAside(link, filepath.Base(link)); err != nil {
			return err
		}
	}
	if err := removeAny(link); err != nil {
		return err
	}
	return host.LinkDir(link, target)
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

// removeDanglingLinks drops each agent's links to skills the store no longer has, such as parked ones.
// A real folder the user put there is not a link, and stays.
func (p paths) removeDanglingLinks() error {
	for _, dir := range p.agentSkills() {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			link := filepath.Join(dir, entry.Name())
			if _, err := os.Stat(link); isLink(link) && err != nil {
				if err := os.Remove(link); err != nil {
					return err
				}
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
		if strings.HasPrefix(entry.Name(), ".") {
			continue // staging left behind by an interrupted publish, not a skill
		}
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

// park moves skills out of the store and into the parked folder, where they wait to be restored.
// Parking the same name twice keeps both: the earlier copy may be the one you want back.
func (p paths) park(names []string) error {
	for _, name := range names {
		if err := p.parkAside(filepath.Join(p.store, name), name); err != nil {
			return err
		}
	}
	return nil
}

// parkAside moves dir into the parked folder under a free name, so nothing parked is ever overwritten.
func (p paths) parkAside(dir, name string) (err error) {
	_, err = p.parkInto(dir, name)
	return err
}

// parkInto parks dir and reports where it went, which is name with a number when that name is taken.
func (p paths) parkInto(dir, name string) (string, error) {
	if err := os.MkdirAll(p.parked, 0o755); err != nil {
		return "", err
	}
	target := filepath.Join(p.parked, name)
	for n := 1; taken(target); n++ {
		target = filepath.Join(p.parked, fmt.Sprintf("%s-%d", name, n))
	}
	return target, atomicfile.Rename(dir, target)
}

func taken(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// alreadyLinks reports that link is a link pointing at target. Leaving such a link alone keeps a sync
// idempotent, and avoids re-creating a link Windows has not finished deleting yet. Readlink is what
// reads a Windows junction; filepath.EvalSymlinks does not resolve one, it hands the link path back.
func alreadyLinks(link, target string) bool {
	if !isLink(link) {
		return false
	}
	at, err := os.Readlink(link)
	return err == nil && filepath.Clean(at) == filepath.Clean(target)
}
