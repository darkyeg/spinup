package skills

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/darkyeg/spinup/internal/library"
)

// Added is a skill of yours that is now in your library and installed on this machine.
type Added struct {
	Name   string
	Path   string
	Synced Synced
	// Renamed is the frontmatter name when it differs from the folder, which agents key off.
	Renamed string
}

const scaffold = `---
name: %s
description: %s
---

# %s

What this skill does, and the steps to follow.
`

const placeholderDescription = "What this skill is for, so the agent knows when to use it."

// Create writes a new skill of your own into your library and installs it everywhere.
func (m Manager) Create(ctx context.Context, name, description string) (Added, error) {
	if err := checkNames([]string{name}); err != nil {
		return Added{}, err
	}
	dir := m.ownPath(name)
	if _, err := os.Stat(dir); err == nil {
		return Added{}, fmt.Errorf("you already have a skill called %s (%s)", name, dir)
	}
	if description == "" {
		description = placeholderDescription
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Added{}, err
	}
	body := fmt.Sprintf(scaffold, name, description, name)
	if err := os.WriteFile(filepath.Join(dir, skillFile), []byte(body), 0o644); err != nil {
		return Added{}, err
	}
	synced, err := m.Sync(ctx)
	return Added{Name: name, Path: dir, Synced: synced}, err
}

// Import copies a skill folder or .zip into your library and installs it everywhere.
func (m Manager) Import(ctx context.Context, from string) (Added, error) {
	sent, err := readSkill(from)
	if err != nil {
		return Added{}, err
	}
	defer sent.close()
	name := sent.name
	if err := checkNames([]string{name}); err != nil {
		return Added{}, err
	}
	dir := m.ownPath(name)
	if _, err := os.Stat(dir); err == nil {
		return Added{}, fmt.Errorf("you already have a skill called %s (%s); remove that folder to replace it", name, dir)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return Added{}, err
	}
	if err := os.CopyFS(dir, sent.files); err != nil {
		return Added{}, err
	}
	added := Added{Name: name, Path: dir}
	text, _ := os.ReadFile(filepath.Join(dir, skillFile))
	if declared := parseFrontmatter(string(text)).name; declared != "" && declared != name {
		added.Renamed = declared
	}
	added.Synced, err = m.Sync(ctx)
	return added, err
}

func (m Manager) ownPath(name string) string {
	return m.lib.Path(library.SkillsDir + "/" + name)
}

// sentSkill is a skill someone sent, open for copying; close releases the .zip it may come from.
type sentSkill struct {
	files fs.FS
	name  string
	close func()
}

// readSkill opens a skill folder or .zip and reports the files to copy and the folder name to use.
func readSkill(from string) (sentSkill, error) {
	info, err := os.Stat(from)
	if err != nil {
		return sentSkill{}, fmt.Errorf("%s: %w", from, err)
	}
	if info.IsDir() {
		if !fileExists(filepath.Join(from, skillFile)) {
			return sentSkill{}, fmt.Errorf("%s has no %s, so it isn't a skill", from, skillFile)
		}
		return sentSkill{os.DirFS(from), filepath.Base(filepath.Clean(from)), func() {}}, nil
	}
	if !strings.EqualFold(filepath.Ext(from), ".zip") {
		return sentSkill{}, fmt.Errorf("%s is neither a folder nor a .zip", from)
	}
	return readZippedSkill(from)
}

func readZippedSkill(from string) (sentSkill, error) {
	archive, err := zip.OpenReader(from)
	if err != nil {
		return sentSkill{}, fmt.Errorf("%s: %w", from, err)
	}
	close := func() { archive.Close() }
	root, err := skillRoot(archive)
	if err != nil {
		close()
		return sentSkill{}, fmt.Errorf("%s: %w", from, err)
	}
	name := path.Base(root)
	if root == "." {
		name = strings.TrimSuffix(filepath.Base(from), filepath.Ext(from))
	}
	files, err := fs.Sub(archive, root)
	if err != nil {
		close()
		return sentSkill{}, err
	}
	return sentSkill{files, name, close}, nil
}

// skillRoot is the shallowest folder in the archive that holds a SKILL.md.
func skillRoot(archive fs.FS) (string, error) {
	best := ""
	err := fs.WalkDir(archive, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Base(p) != skillFile {
			return err
		}
		if dir := path.Dir(p); best == "" || len(dir) < len(best) {
			best = dir
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if best == "" {
		return "", fmt.Errorf("no %s inside", skillFile)
	}
	return best, nil
}

// Status is what every skill is doing right now, without changing anything.
type Status struct {
	Skills   []Skill
	Unlisted []string
	// AgentOnly are skills an agent has on its own, which spinup never installed or shares.
	AgentOnly []string
	Clashes   []Clash
	Library   string
	// Store is where the installed copies live, so an unlisted skill can be named as a path to keep.
	Store string
}

// Missing are the listed skills no agent can load yet, because this machine hasn't installed them.
func (s Status) Missing() []string {
	var missing []string
	for _, skill := range s.Skills {
		if !skill.Installed {
			missing = append(missing, skill.Name)
		}
	}
	return missing
}

// UpToDate reports that a sync would change nothing.
func (s Status) UpToDate() bool { return len(s.Missing()) == 0 && len(s.Unlisted) == 0 }

// Status reads the list, your own skills and what is installed; it never installs or parks.
func (m Manager) Status() (Status, error) {
	list, err := m.List()
	if err != nil {
		return Status{}, err
	}
	unlisted, err := m.Unlisted()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Status{}, err
	}
	installed, err := m.paths.installed()
	if err != nil {
		return Status{}, err
	}
	entries, err := m.Inventory()
	if err != nil {
		return Status{}, err
	}
	var agentOnly []string
	for _, entry := range entries {
		if entry.State == AgentOnly {
			agentOnly = append(agentOnly, entry.Name)
		}
	}
	return Status{
		Skills:    list,
		Unlisted:  unlisted,
		AgentOnly: agentOnly,
		Clashes:   clashes(m.paths.store, installed),
		Library:   m.lib.Dir(),
		Store:     m.paths.store,
	}, nil
}
