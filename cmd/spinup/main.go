// Command spinup sets up every machine you code with AI on, keeps them the same, and shares your AI
// accounts between them without logging them out.
package main

import (
	"cmp"
	"errors"
	"os"

	"github.com/alecthomas/kong"
)

// version is set by the release build.
var version = "dev"

// releases is the GitHub project spinup's releases come from.
var releases = cmp.Or(os.Getenv("SPINUP_RELEASES"), "darkyeg/spinup")

type cli struct {
	Setup     setupCmd     `cmd:"" group:"machine" help:"Set up this machine end to end, or repair it."`
	Doctor    doctorCmd    `cmd:"" group:"machine" help:"Check this machine; every problem comes with its fix."`
	Update    updateCmd    `cmd:"" group:"machine" help:"Update spinup, CLIProxyAPI, skills and Tailscale."`
	Machines  machinesCmd  `cmd:"" group:"machine" help:"Every device on your tailnet and its addresses."`
	Tools     toolsCmd     `cmd:"" group:"machine" help:"Install the dev tools this machine lacks."`
	Uninstall uninstallCmd `cmd:"" group:"machine" help:"Stop the accounts service here. Logins and keys stay."`

	Status   statusCmd   `cmd:"" group:"accounts" help:"Who holds the accounts, every login and every machine."`
	Handoff  handoffCmd  `cmd:"" group:"accounts" help:"Move the accounts to another machine, safely."`
	Takeover takeoverCmd `cmd:"" group:"accounts" help:"Hold the accounts here because their holder is lost for good."`
	Keys     keysCmd     `cmd:"" group:"accounts" help:"Print this machine's API key; holders also print the dashboard password."`

	Skills skillsCmd `cmd:"" group:"agents" help:"Install your skills for Claude Code and Codex; list, add, remove."`
	Agents agentsCmd `cmd:"" group:"agents" help:"Install the shared instructions, subagents and settings."`
	Repo   repoCmd   `cmd:"" group:"agents" help:"Get a project ready for agents: its stack's skills, AGENTS.md, remote."`

	Daemon daemonCmd `cmd:"" hidden:"" help:"Run the accounts service in the foreground (what the boot task runs)."`

	Version kong.VersionFlag `short:"v" help:"Show the version."`
}

const description = `Set up every machine you code with AI on, keep them the same, and share your AI accounts
between them without ever logging them out.

Start with: spinup setup <name-for-this-machine>

Guide: https://github.com/darkyeg/spinup`

func main() {
	var c cli
	ctx := kong.Parse(&c,
		kong.Name("spinup"),
		kong.Description(description),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true, NoExpandSubcommands: true}),
		kong.ExplicitGroups([]kong.Group{
			{Key: "machine", Title: "This machine"},
			{Key: "accounts", Title: "Accounts"},
			{Key: "agents", Title: "Agents"},
		}),
		kong.Vars{"version": "spinup " + version},
	)
	err := ctx.Run()
	if child := (childExit{}); errors.As(err, &child) {
		os.Exit(child.code)
	}
	ctx.FatalIfErrorf(err)
}
