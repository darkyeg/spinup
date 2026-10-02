package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/darkyeg/spinup/internal/project"
)

type repoCmd struct {
	Path  string `arg:"" optional:"" default:"." type:"path" help:"The project's root folder (default: here)."`
	Apply bool   `help:"Make the changes; without it, only report them."`
}

func (repoCmd) Help() string {
	return `Gets a project ready for agents: finds its stack, adds the stack's skills to the project,
checks AGENTS.md's size and that the git remote is a github.com URL. Commit the result in the project.`
}

func (c repoCmd) Run() error {
	ctx := context.Background()
	r, err := project.Inspect(ctx, repoData(), c.Path, project.SystemRunner{})
	if err != nil {
		return err
	}
	printProject(r)
	switch {
	case len(r.Actions) == 0:
		fmt.Println("\nNothing to do.")
	case !c.Apply:
		fmt.Printf("\nRe-run with --apply to: %s.\n", strings.Join(actionNames(r.Actions), "; "))
	default:
		for _, a := range r.Actions {
			outcome, err := a.Apply(ctx)
			if err != nil {
				return fmt.Errorf("%s: %w", a.Description, err)
			}
			step("%s", outcome)
		}
		fmt.Println("\nApplied. Review and commit the changes in the project (git status).")
	}
	return nil
}

func printProject(r project.Report) {
	fmt.Printf("Project:   %s\n", r.Root)
	fmt.Printf("Stack:     %s\n", orNone(strings.Join(r.Stacks, ", "), "nothing recognised"))
	switch {
	case r.Remote.Problem != "":
		fmt.Printf("Remote:    %s\n", r.Remote.Problem)
	case r.Remote.Rewrite != "":
		fmt.Printf("Remote:    %s -> %s (T3 Code groups a repo across machines by its github.com URL)\n", r.Remote.URL, r.Remote.Rewrite)
	default:
		fmt.Printf("Remote:    %s ok\n", r.Remote.URL)
	}
	switch {
	case !r.AgentsMD.Present:
		fmt.Println(`AGENTS.md: missing. Ask an agent: "write an AGENTS.md for this repo (use the writing-for-agents skill)"`)
	case r.AgentsMD.TooLong():
		fmt.Printf("AGENTS.md: %d lines (over %d: trim it, it loads every session)\n", r.AgentsMD.Lines, project.MaxAgentsLines)
	default:
		fmt.Printf("AGENTS.md: %d lines ok\n", r.AgentsMD.Lines)
	}
	for _, n := range r.Notes {
		fmt.Printf("Note (%s): %s\n", n.Stack, n.Text)
	}
	for _, g := range r.Skills {
		fmt.Printf("Skills (%s): %s from %s\n", g.Stack, strings.Join(g.Missing, ", "), g.Source)
	}
}

func actionNames(actions []project.Action) []string {
	names := make([]string, len(actions))
	for i, a := range actions {
		names[i] = a.Description
	}
	return names
}

func orNone(s, none string) string {
	if s == "" {
		return none
	}
	return s
}
