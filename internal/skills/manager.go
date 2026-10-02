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
	"github.com/darkyeg/spinup/internal/shell"
	"github.com/darkyeg/spinup/internal/source"
)

// ParkingSkipped is said when Sync leaves unlisted skills alone because it can only see the built-in list.
const ParkingSkipped = "parking skipped: no spinup checkout here"

// OwnSource is the Source of skills that ship as folders instead of coming from a skills repo.
const OwnSource = "own (skills/local, local/skills)"

const (
	sharedManifest  = "skills/skills.json"
	privateManifest = "skills.json"
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

// Scope picks which skills list an edit changes: the spinup repo's, or your private one.
type Scope int

const (
	Shared Scope = iota
	Private
)

func (s Scope) String() string {
	if s == Private {
		return "private"
	}
	return "shared"
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

type installer func(ctx context.Context, source string, agents, names []string) (output string, err error)

type Manager struct {
	src     source.Source
	paths   paths
	install installer
	logf    func(format string, a ...any)
}

// New reports progress through logf, which may be nil.
func New(src source.Source, logf func(format string, a ...any)) Manager {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return Manager{src: src, paths: hostPaths(), install: npxInstall, logf: logf}
}

// Sync installs every listed skill, links your own, parks the unlisted and applies the modes.
// When an install fails it still returns what was done, with an error naming the failed sources.
func (m Manager) Sync(ctx context.Context) (Synced, error) {
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
	synced := Synced{Count: len(keep), Agents: list.agents, Parked: parked, Failed: failed}
	if len(failed) > 0 {
		return synced, fmt.Errorf("install failed for: %s (nothing was parked)", strings.Join(failed, ", "))
	}
	return synced, nil
}

func (m Manager) installSources(ctx context.Context, list manifest) (failed []string) {
	for _, entry := range list.sources {
		m.logf("Skills from %s: %s", entry.Source, strings.Join(entry.Names, ", "))
		output, err := m.install(ctx, entry.Source, list.agents, entry.Names)
		if err != nil {
			failed = append(failed, entry.Source)
			m.logf("%s", tail(output+err.Error(), failureTailSize))
		}
	}
	return failed
}

func (m Manager) publishOwn(ctx context.Context) ([]ownSkill, error) {
	own, err := findOwn(m.src.Data(), m.src.Private())
	if err != nil {
		return nil, err
	}
	for _, dir := range []string{m.paths.store, m.paths.claudeSkills()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
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
	if !m.seesOwnSkills() {
		m.logf("%s", ParkingSkipped)
		return nil, nil
	}
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

// seesOwnSkills is false for the built-in copy, which can't see the skills kept in a checkout or the private repo.
func (m Manager) seesOwnSkills() bool {
	_, err := m.src.Checkout()
	return err == nil
}

// Unlisted returns the installed skills that Sync would park; without a checkout it can't tell, so none.
func (m Manager) Unlisted() ([]string, error) {
	if !m.seesOwnSkills() {
		return nil, nil
	}
	list, err := m.manifest()
	if err != nil {
		return nil, err
	}
	own, err := findOwn(m.src.Data(), m.src.Private())
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
	own, err := findOwn(m.src.Data(), m.src.Private())
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
func (m Manager) Add(ctx context.Context, from string, names []string, mode Mode, scope Scope) (Synced, error) {
	err := m.edit(scope, func(d *document) error {
		d.add(from, names, mode)
		return nil
	})
	if err != nil {
		return Synced{}, err
	}
	return m.Sync(ctx)
}

// Remove unlists names, then runs Sync, which parks them.
func (m Manager) Remove(ctx context.Context, names []string, scope Scope) (Synced, error) {
	err := m.edit(scope, func(d *document) error {
		if !d.remove(names) {
			return fmt.Errorf("none of %s is in the %s skills list", strings.Join(names, ", "), scope)
		}
		return nil
	})
	if err != nil {
		return Synced{}, err
	}
	return m.Sync(ctx)
}

// SetMode switches names between auto and manual and re-applies the modes without installing.
func (m Manager) SetMode(names []string, mode Mode, scope Scope) error {
	err := m.edit(scope, func(d *document) error {
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
	own, err := findOwn(m.src.Data(), m.src.Private())
	if err != nil {
		return err
	}
	if err := m.paths.applyModes(keepList(list, own), list.manual); err != nil {
		return err
	}
	m.logf("%s: %s. Restart Claude Code / Codex to apply.", strings.Join(names, ", "), mode)
	return nil
}

func (m Manager) edit(scope Scope, change func(*document) error) error {
	path, err := m.manifestPath(scope)
	if err != nil {
		return err
	}
	doc, err := readDocument(os.DirFS(filepath.Dir(path)), filepath.Base(path))
	if err != nil {
		return err
	}
	if err := change(&doc); err != nil {
		return err
	}
	if err := atomicfile.Write(path, doc.render(), 0o644); err != nil {
		return err
	}
	m.logf("Updated %s", path)
	return nil
}

func (m Manager) manifestPath(scope Scope) (string, error) {
	if scope == Private {
		dir, ok := m.src.PrivatePath()
		if !ok {
			return "", errors.New("there is no private repo (local/) to hold your own skills list")
		}
		return filepath.Join(dir, privateManifest), nil
	}
	root, err := m.src.Checkout()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, filepath.FromSlash(sharedManifest)), nil
}

func (m Manager) manifest() (manifest, error) {
	shared, err := readDocument(m.src.Data(), sharedManifest)
	if err != nil {
		return manifest{}, err
	}
	var private document
	if root := m.src.Private(); root != nil {
		if private, err = readDocument(root, privateManifest); err != nil {
			return manifest{}, err
		}
	}
	return merge(shared, private), nil
}

func readDocument(root fs.FS, name string) (document, error) {
	data, err := fs.ReadFile(root, name)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return document{}, err
	}
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
