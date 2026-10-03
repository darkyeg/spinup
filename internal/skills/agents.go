package skills

import "path/filepath"

// Agent is one of the agents spinup installs skills for.
type Agent struct {
	// Name is what the skills CLI and your list call it.
	Name string
	home string
}

// SkillsDir is where this agent looks for skills.
func (a Agent) SkillsDir() string { return filepath.Join(a.home, agentSkillsName) }

// Here reports that the agent is on this machine, which is what its home folder says.
func (a Agent) Here() bool { return dirExists(a.home) }

func (p paths) agents() []Agent {
	return []Agent{{Name: "claude-code", home: p.claude}, {Name: "codex", home: p.codex}}
}

// agentsHere are the agents to install for. Nobody declares them: spinup looks for their homes. Before
// either agent is installed, which is how `spinup setup` runs, it prepares both rather than skipping all.
func (p paths) agentsHere() []Agent {
	var here []Agent
	for _, agent := range p.agents() {
		if agent.Here() {
			here = append(here, agent)
		}
	}
	if len(here) == 0 {
		return p.agents()
	}
	return here
}

// agentSkills are the skills folders of the agents on this machine; spinup links each one at the store.
func (p paths) agentSkills() []string {
	var dirs []string
	for _, agent := range p.agentsHere() {
		dirs = append(dirs, agent.SkillsDir())
	}
	return dirs
}

func agentNames(agents []Agent) []string {
	names := make([]string, 0, len(agents))
	for _, agent := range agents {
		names = append(names, agent.Name)
	}
	return names
}
