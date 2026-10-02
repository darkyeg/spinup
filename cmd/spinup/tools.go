package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/darkyeg/spinup/internal/host"
	"github.com/darkyeg/spinup/internal/source"
	"github.com/darkyeg/spinup/internal/tools"
)

type toolsCmd struct {
	Check bool `help:"Only list what's missing."`
}

func (toolsCmd) Help() string {
	return `Installs the dev tools in tools.json that this machine lacks, with winget, brew, apt or the
vendor's installer. On NixOS it prints the configuration.nix line instead.`
}

func (c toolsCmd) Run() error {
	repo := repoData()
	if c.Check {
		_, err := missingTools(repo)
		return err
	}
	return installTools(context.Background(), repo)
}

func installTools(ctx context.Context, repo source.Source) error {
	missing, err := missingTools(repo)
	if err != nil || len(missing) == 0 {
		return err
	}
	if host.NixOS() {
		fmt.Println("NixOS: add to environment.systemPackages in configuration.nix, then run `sudo nixos-rebuild switch`:")
		fmt.Println("  " + tools.NixLine(missing))
		return nil
	}
	var failed []string
	for _, r := range tools.Install(ctx, missing) {
		switch r.Outcome {
		case tools.NoInstaller:
			failed = append(failed, r.Tool.Name+" (no installer for this OS)")
		case tools.Failed:
			failed = append(failed, fmt.Sprintf("%s (%v)", r.Tool.Name, r.Err))
		}
	}
	step("Open a new terminal so it finds the new tools")
	if len(failed) > 0 {
		return fmt.Errorf("not installed: %s", strings.Join(failed, ", "))
	}
	return nil
}

func missingTools(repo source.Source) ([]tools.Tool, error) {
	all, err := tools.Load(repo)
	if err != nil {
		return nil, err
	}
	missing := tools.Missing(all)
	if len(missing) == 0 {
		step("All %d tools are installed", len(all))
	} else {
		step("Missing: %s", strings.Join(toolNames(missing), ", "))
	}
	return missing, nil
}

func toolNames(list []tools.Tool) []string {
	names := make([]string, len(list))
	for i, t := range list {
		names[i] = t.Name
	}
	return names
}
