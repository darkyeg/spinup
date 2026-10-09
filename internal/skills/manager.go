// Package skills installs the global skill list for every agent, keeps your own skills linked in,
// parks the rest, and edits the list and each skill's manual or auto mode.
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
	"unicode/utf8"

	"github.com/darkyeg/spinup/internal/atomicfile"
	"github.com/darkyeg/spinup/internal/library"
	"github.com/darkyeg/spinup/internal/shell"
	"github.com/darkyeg/spinup/internal/source"
)

// OwnSource is the Source of skills that live in your library (~/.spinup/skills): yours, and shared between your machines.
const OwnSource = "your library"

const (
	// suggestedList is spinup's list, used until you change yours.
	suggestedList   = "skills/skills.json"
	failureTailSize = 800
	tokenChars      = 4
)

// Mode says whether an agent may pick a skill itself (Auto) or only runs it when called (Manual).
type Mode int

const (
	Auto Mode = iota
	Manual
)

func (m Mode) String() string {
	if m == Manual {
		return "manual"
	}
	return "auto"
}

type Skill struct {
	Name             string
	Source           string
	Mode             Mode
	Installed        bool
	DescriptionChars int
}

// TokenEstimate is what the installed auto skills' descriptions cost in every session.
func TokenEstimate(skills []Skill) int {
	chars := 0
	for _, s := range skills {
		if s.Mode == Auto && s.Installed {
			chars += s.DescriptionChars
		}
	}
	return chars / tokenChars
}

// Synced reports a finished Sync.
type Synced struct {
	Count  int
	Agents []string
	Parked []string
	Failed []string
}

// installer fetches skills from a GitHub repo into the skill store; nil means this machine doesn't fetch.
type installer func(ctx context.Context, source string, agents, names []string) (output string, err error)

type Manager struct {
	src     source.Source
	lib     library.Library
	paths   paths
	install installer
	logf    func(format string, a ...any)
}

// New reads spinup's suggested list from src and yours from lib; it reports progress through logf, which may be nil.
func New(src source.Source, lib library.Library, logf func(format string, a ...any)) Manager {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return Manager{src: src, lib: lib, paths: hostPaths(), install: npxInstall, logf: logf}
}

// Offline never fetches: it installs only the copies your library already has. The service uses it,
// since it may run without Node.js or a network.
func (m Manager) Offline() Manager {
	m.install = nil
	return m
}

// Sync installs every listed skill, links your own, parks the unlisted and applies the modes.
// When an install fails it still returns what was done, with an error naming the failed sources.
func (m Manager) Sync(ctx context.Context) (Synced, error) {
	lock, err := m.paths.lockStore(ctx)
	if err != nil {
		return Synced{}, err
	}
	defer lock.Close()
	list, err := m.manifest()
	if err != nil {
		return Synced{}, err
	}
	failed := m.installSources(ctx, list)
	own, err := m.publishOwn(ctx)
	if err != nil {
		return Synced{}, err
	}
	keep := keepList(list, own)
	if err := m.paths.linkAll(ctx, keep); err != nil {
		return Synced{}, err
	}
	parked, err := m.parkUnlisted(keep, len(failed))
	if err != nil {
		return Synced{}, err
	}
	if err := m.paths.removeDanglingLinks(); err != nil {
		return Synced{}, err
	}
	if err := m.paths.applyModes(keep, list.manual); err != nil {
		return Synced{}, err
	}
	synced := Synced{Count: len(keep), Agents: agentNames(m.paths.agentsHere()), Parked: parked, Failed: failed}
	if len(failed) > 0 {
		return synced, fmt.Errorf("install failed for: %s (nothing was parked)", strings.Join(failed, ", "))
	}
	return synced, nil
}

// installSources installs each listed skill from your library's copy, fetching (and keeping a copy of)
// only the ones the library doesn't have yet.
func (m Manager) installSources(ctx context.Context, list manifest) (failed []string) {
	for _, entry := range list.sources {
		if err := m.fetchMissing(ctx, entry, agentNames(m.paths.agentsHere())); err != nil {
			failed = append(failed, entry.Source)
			m.logf("%s", err)
			continue
		}
		if err := m.installFetched(ctx, entry.Names); err != nil {
			failed = append(failed, entry.Source)
			m.logf("%s: %v", entry.Source, err)
		}
	}
	return failed
}

func (m Manager) fetchMissing(ctx context.Context, entry sourceEntry, agents []string) error {
	var missing []string
	for _, name := range entry.Names {
		if !fileExists(filepath.Join(m.fetchedCopy(name), skillFile)) {
			missing = append(missing, name)
		}
	}
	switch {
	case len(missing) == 0:
		return nil
	case m.install == nil:
		return fmt.Errorf("%s: %s not in your library yet; run `spinup skills sync` on a machine with Node.js",
			entry.Source, strings.Join(missing, ", "))
	}
	m.logf("Fetching from %s: %s", entry.Source, strings.Join(missing, ", "))
	if output, err := m.install(ctx, entry.Source, agents, missing); err != nil {
		return fmt.Errorf("%s", tail(output+err.Error(), failureTailSize))
	}
	for _, name := range missing {
		if err := m.adopt(name); err != nil {
			return fmt.Errorf("%s: keeping a copy of %s: %w", entry.Source, name, err)
		}
	}
	return nil
}

// adopt settles a freshly fetched skill under the name you asked for, then keeps a copy for your other
// machines. A skill's folder and the name in its SKILL.md can differ, and the list goes by the name.
func (m Manager) adopt(name string) error {
	folder, err := m.fetchedFolder(name)
	if err != nil {
		return err
	}
	if folder != name {
		under := filepath.Join(m.paths.store, name)
		if err := removeAny(under); err != nil {
			return err
		}
		if err := atomicfile.Rename(filepath.Join(m.paths.store, folder), under); err != nil {
			return fmt.Errorf("it arrived as %q: %w", folder, err)
		}
		m.logf("%s arrived in a folder called %s; keeping it as %s.", name, folder, name)
	}
	return m.keepCopy(name)
}

// fetchedFolder is the folder the fetch left the skill in: the one named after it, or the one whose
// SKILL.md calls itself name.
func (m Manager) fetchedFolder(name string) (string, error) {
	if fileExists(filepath.Join(m.paths.store, name, skillFile)) {
		return name, nil
	}
	installed, err := m.paths.installed()
	if err != nil {
		return "", err
	}
	for _, folder := range installed {
		if calledName(m.paths.store, folder) == name {
			return folder, nil
		}
	}
	return "", fmt.Errorf("it isn't in %s after fetching; check that the repo has a skill called %q", m.paths.store, name)
}

// keepCopy saves a freshly fetched skill into your library, for your other machines; a copy is whole or absent.
func (m Manager) keepCopy(name string) error {
	installed := filepath.Join(m.paths.store, name)
	if !fileExists(filepath.Join(installed, skillFile)) {
		return fmt.Errorf("it isn't in %s after fetching", m.paths.store)
	}
	dest := m.fetchedCopy(name)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(filepath.Dir(dest), "."+name+"-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := os.CopyFS(filepath.Join(staging, name), os.DirFS(installed)); err != nil {
		return err
	}
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	return atomicfile.Rename(filepath.Join(staging, name), dest)
}

// installFetched installs the library's copy of each skill this machine doesn't have installed.
func (m Manager) installFetched(ctx context.Context, names []string) error {
	for _, name := range names {
		if fileExists(filepath.Join(m.paths.store, name, skillFile)) {
			continue
		}
		if err := m.paths.publish(ctx, ownSkill{name, os.DirFS(m.fetchedCopy(name))}); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func (m Manager) fetchedCopy(name string) string {
	return m.lib.Path(library.Fetched + "/" + name)
}

func (m Manager) publishOwn(ctx context.Context) ([]ownSkill, error) {
	own, err := findOwn(m.lib.Files())
	if err != nil {
		return nil, err
	}
	for _, skill := range own {
		if err := m.paths.publish(ctx, skill); err != nil {
			return nil, fmt.Errorf("own skill %s: %w", skill.name, err)
		}
	}
	if len(own) > 0 {
		m.logf("Own skills: %s", strings.Join(ownNames(own), ", "))
	}
	return own, nil
}

func (m Manager) parkUnlisted(keep []string, failedInstalls int) ([]string, error) {
	installed, err := m.paths.installed()
	if err != nil {
		return nil, err
	}
	parked := parkable(installed, keep, failedInstalls)
	if err := m.paths.park(parked); err != nil {
		return nil, err
	}
	if len(parked) > 0 {
		m.logf("Parked %d unlisted skills in %s (move one back to re-enable it)", len(parked), m.paths.parked)
	}
	return parked, nil
}

// Unlisted returns the installed skills that Sync would park.
func (m Manager) Unlisted() ([]string, error) {
	list, err := m.manifest()
	if err != nil {
		return nil, err
	}
	own, err := findOwn(m.lib.Files())
	if err != nil {
		return nil, err
	}
	installed, err := m.paths.installed()
	if err != nil {
		return nil, err
	}
	return parkable(installed, keepList(list, own), 0), nil
}

// List returns every listed and own skill, sorted by name.
func (m Manager) List() ([]Skill, error) {
	list, err := m.manifest()
	if err != nil {
		return nil, err
	}
	own, err := findOwn(m.lib.Files())
	if err != nil {
		return nil, err
	}
	sourceOf := map[string]string{}
	for _, entry := range list.sources {
		for _, name := range entry.Names {
			sourceOf[name] = entry.Source
		}
	}
	for _, skill := range own {
		sourceOf[skill.name] = OwnSource
	}
	var skills []Skill
	for name, from := range sourceOf {
		skills = append(skills, m.describe(name, from, list.manual))
	}
	slices.SortFunc(skills, func(a, b Skill) int { return strings.Compare(a.Name, b.Name) })
	return skills, nil
}

func (m Manager) describe(name, from string, manual []string) Skill {
	skill := Skill{Name: name, Source: from}
	skill.Installed = dirExists(filepath.Join(m.paths.store, name))
	text, _ := os.ReadFile(filepath.Join(m.paths.store, name, skillFile))
	head := parseFrontmatter(string(text))
	skill.DescriptionChars = utf8.RuneCountInString(head.description)
	if slices.Contains(manual, name) || head.authorManual {
		skill.Mode = Manual
	}
	return skill
}

// Add lists names from source, as manual skills when mode is Manual, then runs Sync.
func (m Manager) Add(ctx context.Context, from string, names []string, mode Mode) (Synced, error) {
	if err := checkNames(names); err != nil {
		return Synced{}, err
	}
	err := m.edit(func(d *document) error {
		d.add(from, names, mode)
		return nil
	})
	if err != nil {
		return Synced{}, err
	}
	return m.Sync(ctx)
}

// Remove unlists names and drops the library folder of any that are your own, then runs Sync, which
// parks the installed copies. Nothing is lost: the parked copy is the skill as it was.
func (m Manager) Remove(ctx context.Context, names []string) (Synced, error) {
	mine, listed, err := m.splitOwn(names)
	if err != nil {
		return Synced{}, err
	}
	if len(mine) == 0 && len(listed) == 0 {
		return Synced{}, fmt.Errorf("none of %s is in your skills list or your own skills",
			strings.Join(names, ", "))
	}
	if len(listed) > 0 {
		if err := m.edit(func(d *document) error { d.remove(listed); return nil }); err != nil {
			return Synced{}, err
		}
	}
	for _, name := range mine {
		if err := os.RemoveAll(m.ownPath(name)); err != nil {
			return Synced{}, fmt.Errorf("removing your skill %s: %w", name, err)
		}
	}
	return m.Sync(ctx)
}

// splitOwn sorts names into the ones that are your own skills and the ones on your list, dropping any
// that are neither so the caller can tell nothing was matched.
func (m Manager) splitOwn(names []string) (mine, listed []string, err error) {
	own, err := findOwn(m.lib.Files())
	if err != nil {
		return nil, nil, err
	}
	onList, err := m.manifest()
	if err != nil {
		return nil, nil, err
	}
	for _, name := range names {
		switch {
		case slices.ContainsFunc(own, func(s ownSkill) bool { return s.name == name }):
			mine = append(mine, name)
		case slices.Contains(onList.listed(), name):
			listed = append(listed, name)
		}
	}
	return mine, listed, nil
}

// SetMode switches names between auto and manual and re-applies the modes without installing.
func (m Manager) SetMode(names []string, mode Mode) error {
	if err := checkNames(names); err != nil {
		return err
	}
	err := m.edit(func(d *document) error {
		d.setMode(names, mode)
		return nil
	})
	if err != nil {
		return err
	}
	list, err := m.manifest()
	if err != nil {
		return err
	}
	own, err := findOwn(m.lib.Files())
	if err != nil {
		return err
	}
	if err := m.paths.applyModes(keepList(list, own), list.manual); err != nil {
		return err
	}
	m.logf("%s: %s. Restart Claude Code / Codex to apply.", strings.Join(names, ", "), mode)
	return nil
}

// edit changes your list; the first edit starts it as a copy of spinup's.
func (m Manager) edit(change func(*document) error) error {
	doc, err := m.document()
	if err != nil {
		return err
	}
	doc.obj.remove(keyComment)
	if err := change(&doc); err != nil {
		return err
	}
	path := m.lib.Path(library.SkillsList)
	if err := atomicfile.Write(path, doc.render(), 0o644); err != nil {
		return err
	}
	m.logf("Updated %s", path)
	return nil
}

func (m Manager) manifest() (manifest, error) {
	doc, err := m.document()
	return doc.manifest(), err
}

// document is your list when you have one, otherwise spinup's.
func (m Manager) document() (document, error) {
	data, err := fs.ReadFile(m.lib.Files(), library.SkillsList)
	if errors.Is(err, fs.ErrNotExist) {
		return readDocument(m.src.Data(), suggestedList)
	}
	if err != nil {
		return document{}, err
	}
	return parsed(m.lib.Path(library.SkillsList), data)
}

func readDocument(root fs.FS, name string) (document, error) {
	data, err := fs.ReadFile(root, name)
	if err != nil {
		return document{}, err
	}
	return parsed(name, data)
}

func parsed(name string, data []byte) (document, error) {
	doc, err := parseDocument(data)
	if err != nil {
		return document{}, fmt.Errorf("%s: %w", name, err)
	}
	return doc, nil
}

func keepList(list manifest, own []ownSkill) []string {
	keep := append(list.listed(), ownNames(own)...)
	slices.Sort(keep)
	return slices.Compact(keep)
}

func ownNames(own []ownSkill) []string {
	names := make([]string, len(own))
	for i, skill := range own {
		names[i] = skill.name
	}
	return names
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func tail(text string, size int) string {
	text = strings.TrimSpace(text)
	if len(text) > size {
		return text[len(text)-size:]
	}
	return text
}

func npxInstall(ctx context.Context, from string, agents, names []string) (string, error) {
	npx, ok := shell.Find("npx")
	if !ok {
		shell.RefreshPath()
		if npx, ok = shell.Find("npx"); !ok {
			return "", errors.New("Node.js is needed for skills (npx not found). Install Node.js, then re-run")
		}
	}
	args := []string{"--yes", "skills", "add", from, "-g"}
	for _, agent := range agents {
		args = append(args, "-a", agent)
	}
	for _, name := range names {
		args = append(args, "-s", name)
	}
	return shell.Output(ctx, npx, append(args, "-y")...)
}
