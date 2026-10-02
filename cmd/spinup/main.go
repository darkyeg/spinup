// Command spinup keeps your AI accounts usable on all your machines without logging them out.
package main

import "github.com/alecthomas/kong"

// version is set by the release build.
var version = "dev"

type cli struct {
	Install   installCmd   `cmd:"" group:"setup" help:"Run spinup on this machine, now and at every boot."`
	Uninstall uninstallCmd `cmd:"" group:"setup" help:"Stop running spinup here. Logins and keys stay."`
	Status    statusCmd    `cmd:"" group:"accounts" help:"Who holds the accounts, every login and every machine."`
	Handoff   handoffCmd   `cmd:"" group:"accounts" help:"Move the accounts to another machine, safely."`
	Takeover  takeoverCmd  `cmd:"" group:"accounts" help:"Hold the accounts here because their holder is lost for good."`
	Daemon    daemonCmd    `cmd:"" hidden:"" help:"Run the service in the foreground (what the boot task runs)."`

	Version kong.VersionFlag `short:"v" help:"Show the version."`
}

const description = `Your AI accounts on all your machines, never logged out.

Every machine uses the accounts at http://localhost:8317. One machine holds them (the hub);
a standby takes them over when the hub is off, and gives them back when it returns.

Guide: https://github.com/darkyeg/spinup/blob/main/docs/SERVICE.md`

func main() {
	var c cli
	ctx := kong.Parse(&c,
		kong.Name("spinup"),
		kong.Description(description),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
		kong.ExplicitGroups([]kong.Group{
			{Key: "setup", Title: "Set up this machine"},
			{Key: "accounts", Title: "Accounts"},
		}),
		kong.Vars{"version": "spinup " + version},
	)
	ctx.FatalIfErrorf(ctx.Run())
}
