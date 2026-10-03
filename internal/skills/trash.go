package skills

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Parked is a skill that was taken off your list or removed, waiting in the parked folder. Nothing
// here is deleted, so a removal you regret is one `restore` away.
type Parked struct {
	// Name is the folder in the parked folder, which gets a number when the name was already taken.
	Name string
	// Was is the skill's own name, before parking numbered it.
	Was     string
	Removed time.Time
	Path    string
}

// Trash lists the parked skills, newest first.
func (m Manager) Trash() ([]Parked, error) {
	entries, err := os.ReadDir(m.paths.parked)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var parked []Parked
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		parked = append(parked, Parked{
			Name:    entry.Name(),
			Was:     withoutCopyNumber(entry.Name()),
			Removed: info.ModTime(),
			Path:    filepath.Join(m.paths.parked, entry.Name()),
		})
	}
	slices.SortFunc(parked, func(a, b Parked) int { return b.Removed.Compare(a.Removed) })
	return parked, nil
}

// withoutCopyNumber turns the "review-team-2" that parking made back into "review-team".
func withoutCopyNumber(name string) string {
	base, number, found := strings.Cut(reverse(name), "-")
	if !found || number == "" {
		return name
	}
	for _, r := range base {
		if r < '0' || r > '9' {
			return name
		}
	}
	return reverse(number)
}

func reverse(s string) string {
	r := []rune(s)
	slices.Reverse(r)
	return string(r)
}

// Restore takes a parked skill back into your library as one of your own, so it reaches every machine.
func (m Manager) Restore(ctx context.Context, name string) (Added, error) {
	from := filepath.Join(m.paths.parked, name)
	if !fileExists(filepath.Join(from, skillFile)) {
		return Added{}, fmt.Errorf("%s is not in %s", name, m.paths.parked)
	}
	under := withoutCopyNumber(name)
	if err := checkNames([]string{under}); err != nil {
		return Added{}, err
	}
	dir := m.ownPath(under)
	if _, err := os.Stat(dir); err == nil {
		return Added{}, fmt.Errorf("you already have a skill called %s (%s)", under, dir)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return Added{}, err
	}
	if err := os.CopyFS(dir, os.DirFS(from)); err != nil {
		return Added{}, err
	}
	if err := os.RemoveAll(from); err != nil {
		return Added{}, err
	}
	synced, err := m.Sync(ctx)
	return Added{Name: under, Path: dir, Synced: synced}, err
}

// Adopt takes skills that exist only on this machine into your library, so a sync keeps them and your
// other machines get them too. They may be installed by spinup or belong to an agent alone; an agent's
// own folder is removed once its copy is safely in the library, since the sync links it back.
func (m Manager) Adopt(ctx context.Context, names []string) (Synced, error) {
	for _, name := range names {
		if err := checkNames([]string{name}); err != nil {
			return Synced{}, err
		}
		from, agentCopies := m.localCopy(name)
		if from == "" {
			return Synced{}, fmt.Errorf("%s is not installed here", name)
		}
		dir := m.ownPath(name)
		if _, err := os.Stat(dir); err == nil {
			return Synced{}, fmt.Errorf("you already have a skill called %s (%s)", name, dir)
		}
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return Synced{}, err
		}
		if err := os.CopyFS(dir, os.DirFS(from)); err != nil {
			_ = os.RemoveAll(dir)
			return Synced{}, err
		}
		for _, copy := range agentCopies {
			if err := os.RemoveAll(copy); err != nil {
				return Synced{}, err
			}
		}
	}
	return m.Sync(ctx)
}

// localCopy finds a skill on this machine: in spinup's store, or else in an agent's own folder. It
// also returns the agents' real folders, which become redundant once the library has the skill.
func (m Manager) localCopy(name string) (from string, agentCopies []string) {
	if fileExists(filepath.Join(m.paths.store, name, skillFile)) {
		return filepath.Join(m.paths.store, name), nil
	}
	for _, agent := range m.paths.agentsHere() {
		dir := filepath.Join(agent.SkillsDir(), name)
		if !fileExists(filepath.Join(dir, skillFile)) {
			continue
		}
		if from == "" {
			from = dir
		}
		if !isLink(dir) {
			agentCopies = append(agentCopies, dir)
		}
	}
	return from, agentCopies
}

// Discard takes a skill that is not in your library off this machine, into the parked folder. It
// leaves every other skill alone, which a full sync would not.
func (m Manager) Discard(name string) error {
	if err := checkNames([]string{name}); err != nil {
		return err
	}
	if fileExists(filepath.Join(m.paths.store, name, skillFile)) {
		if err := m.paths.park([]string{name}); err != nil {
			return err
		}
	}
	for _, agent := range m.paths.agentsHere() {
		dir := filepath.Join(agent.SkillsDir(), name)
		switch {
		case isLink(dir):
			if err := os.Remove(dir); err != nil {
				return err
			}
		case fileExists(filepath.Join(dir, skillFile)):
			if err := m.paths.parkAside(dir, name); err != nil {
				return err
			}
		}
	}
	return nil
}
