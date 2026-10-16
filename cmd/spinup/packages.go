package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/darkyeg/spinup/internal/doctor"
	"github.com/darkyeg/spinup/internal/packages"
)

type packagesCmd struct {
	List     packagesListCmd     `cmd:"" default:"1" help:"Show locked packages and each tool's installation state."`
	Plan     packagesListCmd     `cmd:"" help:"Show what sync would change without applying it."`
	Add      packagesAddCmd      `cmd:"" help:"Add a GitHub plugin package, pin its content and install it."`
	Update   packagesUpdateCmd   `cmd:"" help:"Resolve a newer revision of one package; keep the previous revision for rollback."`
	Remove   packagesRemoveCmd   `cmd:"" help:"Remove a package from the library and uninstall owned registrations."`
	Rollback packagesRollbackCmd `cmd:"" help:"Restore the previous locked revision on your machines."`
	Sync     packagesSyncCmd     `cmd:"" help:"Apply locked packages on this machine without fetching newer releases."`
	Fetch    packagesFetchCmd    `cmd:"" help:"Download missing or damaged artifacts at their locked revisions."`
}

func (packagesCmd) Help() string {
	return `Packages keep a plugin's skills, hooks, scripts and assets together. Spinup pins a
GitHub commit and content digest, then installs through each tool's native plugin CLI.

  spinup packages add obra/superpowers
  spinup packages add owner/repo --ref v1.0.0 --agent codex
  spinup packages plan
  spinup packages update obra/superpowers
  spinup packages rollback obra/superpowers
  spinup packages remove obra/superpowers

Use --path plugins/name for a plugin inside a repository. Existing standalone skills
remain managed by spinup skills. Models, logins and personal settings remain local.
Restart the affected tool after changes. See docs/PACKAGES.md for requirements and recovery.`
}

func packageManager() packages.Manager {
	return packages.New(yourLibrary(repoData()), packages.HostTargets())
}

type packagesListCmd struct {
	JSON bool `help:"Print machine-readable installation states."`
}

func (c packagesListCmd) Run() error {
	results, err := packageManager().Plan(context.Background())
	if err != nil {
		return err
	}
	if c.JSON {
		return json.NewEncoder(os.Stdout).Encode(results)
	}
	printPackages(results)
	return nil
}

type packagesAddCmd struct {
	Source string   `arg:"" help:"GitHub owner/repo or repository URL."`
	Ref    string   `default:"HEAD" help:"Branch, tag or commit to resolve once."`
	Path   string   `help:"Plugin directory within the repository."`
	Agent  []string `enum:"claude-code,codex" help:"Target tools (repeat or comma-separate); default: every supported manifest."`
}

func (c packagesAddCmd) Run() error {
	catchUpLibrary()
	ctx := context.Background()
	p, err := packageManager().Add(ctx, c.Source, c.Ref, c.Path, c.Agent)
	if err != nil {
		return err
	}
	printPinned(p)
	return syncPackages(ctx)
}

type packagesUpdateCmd struct {
	Source string `arg:""`
	Ref    string `help:"Change the tracked branch, tag or commit."`
}

func (c packagesUpdateCmd) Run() error {
	catchUpLibrary()
	ctx := context.Background()
	p, err := packageManager().Update(ctx, c.Source, c.Ref)
	if err != nil {
		return err
	}
	printPinned(p)
	return syncPackages(ctx)
}

type packagesRemoveCmd struct {
	Source string `arg:""`
}

func (c packagesRemoveCmd) Run() error {
	catchUpLibrary()
	ctx := context.Background()
	if err := packageManager().Remove(ctx, c.Source); err != nil {
		return err
	}
	return syncPackages(ctx)
}

type packagesRollbackCmd struct {
	Source string `arg:""`
}

func (c packagesRollbackCmd) Run() error {
	catchUpLibrary()
	ctx := context.Background()
	if err := packageManager().Rollback(ctx, c.Source); err != nil {
		return err
	}
	return syncPackages(ctx)
}

type packagesSyncCmd struct{}

func (packagesSyncCmd) Run() error { catchUpLibrary(); return syncPackages(context.Background()) }

type packagesFetchCmd struct{}

func (packagesFetchCmd) Run() error {
	catchUpLibrary()
	ctx := context.Background()
	if err := packageManager().Fetch(ctx); err != nil {
		return err
	}
	return syncPackages(ctx)
}

func syncPackages(ctx context.Context) error {
	results, err := packageManager().Sync(ctx)
	printPackages(results)
	return err
}

func printPinned(p packages.Package) {
	fmt.Printf("Pinned %s %s at %s.\n", p.Source, p.Release.Version, p.Release.Revision[:12])
}

func printPackages(results []packages.Result) {
	if len(results) == 0 {
		fmt.Println("No packages requested. Add one with: spinup packages add owner/repo")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, r := range results {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Source, r.Target, r.Version, r.Action)
		if r.Detail != "" {
			fmt.Fprintf(w, "  %s\n", r.Detail)
		}
	}
	_ = w.Flush()
}

func packagesSection(ctx context.Context) doctor.Section {
	s := doctor.Section{Title: "Packages"}
	results, err := packageManager().Plan(ctx)
	if err != nil {
		s.Checks = append(s.Checks, doctor.Check{Level: doctor.Fail, Label: err.Error(), Fix: "spinup packages plan"})
		return s
	}
	for _, r := range results {
		level := doctor.Fail
		fix := "spinup packages sync"
		if r.Action == packages.Ready {
			level = doctor.OK
			fix = ""
		}
		if r.Action == packages.Disabled {
			level = doctor.Warn
			fix = ""
		}
		s.Checks = append(s.Checks, doctor.Check{Level: level, Label: fmt.Sprintf("%s %s: %s %s", r.Source, r.Target, r.Action, r.Detail), Fix: fix})
	}
	return s
}
