package main

import (
	"github.com/darkyeg/spinup/internal/agentconfig"
	"github.com/darkyeg/spinup/internal/source"
)

type agentsCmd struct{}

func (agentsCmd) Help() string {
	return `Installs agents/ from the spinup repo: one AGENTS.md for Claude Code and Codex (plus your
private local/AGENTS.md), the Claude subagents, and the settings, merged into yours.`
}

func (agentsCmd) Run() error { return installAgentConfig(repoData()) }

func installAgentConfig(repo source.Source) error {
	written, err := agentconfig.Install(repo, agentconfig.HostHomes())
	for _, path := range written {
		step("Wrote %s", path)
	}
	switch {
	case err != nil:
		return err
	case len(written) == 0:
		step("Agent config already matches the repo")
	default:
		step("Restart Claude Code, Codex and T3 Code to pick it up")
	}
	return nil
}
