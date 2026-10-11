package main

import (
	"github.com/darkyeg/spinup/internal/agentconfig"
	"github.com/darkyeg/spinup/internal/source"
)

type agentsCmd struct{}

func (agentsCmd) Help() string {
	return `Installs shared instructions for Claude Code and Codex, followed by your own
AGENTS.md from your library (~/.spinup). Your model choices and settings stay unchanged
unless your spinup fork supplies overrides. No subagents are bundled by default.`
}

func (agentsCmd) Run() error { return installAgentConfig(repoData()) }

func installAgentConfig(repo source.Source) error {
	written, err := agentconfig.Install(agentSources(repo), agentconfig.HostHomes())
	for _, path := range written {
		step("Wrote %s", path)
	}
	switch {
	case err != nil:
		return err
	case len(written) == 0:
		step("Agent config is already up to date")
	default:
		step("Restart Claude Code, Codex and T3 Code to pick it up")
	}
	return nil
}
