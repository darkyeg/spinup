package skills

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// State says who looks after a skill on this machine.
type State int

const (
	// Shared skills are in your library or on your list: spinup installs them and shares them.
	Shared State = iota
	// OnlyHere skills spinup installed on this machine, but they are not in your library, so no
	// other machine gets them and a sync would remove them.
	OnlyHere
	// AgentOnly skills are folders an agent has on its own; spinup never installed them.
	AgentOnly
)

// Entry is one skill as this machine has it.
type Entry struct {
	Name             string
	State            State
	Mode             Mode
	Source           string
	DescriptionChars int
	// Agents are the names of the agents that load the skill.
	Agents []string
	// Path is where an AgentOnly skill's files are.
	Path string
}

// Inventory lists every skill an agent on this machine can load, plus the ones on your list that
// are not installed yet, sorted by name.
func (m Manager) Inventory() ([]Entry, error) {
	listed, err := m.List()
	if err != nil {
		return nil, err
	}
	byName := map[string]*Entry{}
	var entries []*Entry
	add := func(e Entry) *Entry {
		entries = append(entries, &e)
		byName[e.Name] = &e
		return &e
	}
	for _, s := range listed {
		add(Entry{Name: s.Name, State: Shared, Mode: s.Mode, Source: s.Source, DescriptionChars: s.DescriptionChars})
	}
	installed, err := m.paths.installed()
	if err != nil {
		return nil, err
	}
	for _, name := range installed {
		if _, known := byName[name]; !known {
			add(m.describeLocal(name, OnlyHere, filepath.Join(m.paths.store, name)))
		}
	}
	for _, agent := range m.paths.agentsHere() {
		for _, name := range skillFolders(agent.SkillsDir()) {
			entry, known := byName[name]
			if !known {
				entry = add(m.describeLocal(name, AgentOnly, filepath.Join(agent.SkillsDir(), name)))
			}
			if !slices.Contains(entry.Agents, agent.Name) {
				entry.Agents = append(entry.Agents, agent.Name)
			}
		}
	}
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, *e)
	}
	slices.SortFunc(out, func(a, b Entry) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func (m Manager) describeLocal(name string, state State, dir string) Entry {
	text, _ := os.ReadFile(filepath.Join(dir, skillFile))
	head := parseFrontmatter(string(text))
	mode := Auto
	if head.authorManual {
		mode = Manual
	}
	return Entry{Name: name, State: state, Mode: mode, Path: dir, DescriptionChars: len([]rune(head.description))}
}

// skillFolders are the names in dir that hold a SKILL.md, whether they are real folders or links.
// Names starting with a dot belong to the agent itself, such as Codex's .system.
func skillFolders(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if fileExists(filepath.Join(dir, entry.Name(), skillFile)) {
			names = append(names, entry.Name())
		}
	}
	return names
}
