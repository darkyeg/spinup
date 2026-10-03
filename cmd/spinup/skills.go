package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/skills"
	"github.com/darkyeg/spinup/internal/source"
)

type skillsCmd struct {
	Sync   skillsSyncCmd   `cmd:"" default:"1" help:"Install the skills on the list for Claude Code and Codex; park the rest (the default)."`
	List   skillsListCmd   `cmd:"" help:"Every skill: auto or manual, where it comes from, what it costs."`
	Add    skillsAddCmd    `cmd:"" help:"Add skills from a GitHub repo to the list and install them."`
	Remove skillsRemoveCmd `cmd:"" help:"Take skills off the list; they get parked."`
	Manual skillsManualCmd `cmd:"" help:"Make skills run only when you call them (/name, $name); they cost no tokens until then."`
	Auto   skillsAutoCmd   `cmd:"" help:"Let the agent use skills by itself."`
}

func (skillsCmd) Help() string {
	return `Your skills list and your own skills live in your library, ~/.spinup. Until you change the
list, spinup's suggested one is used; your first change copies it there.`
}

type skillsSyncCmd struct{}

func (skillsSyncCmd) Run() error { return syncSkills(context.Background(), repoData()) }

type skillsListCmd struct{}

func (skillsListCmd) Run() error {
	list, err := skillManager(repoData()).List()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, s := range list {
		missing := ""
		if !s.Installed {
			missing = "  (not installed: run spinup skills)"
		}
		fmt.Fprintf(w, "  %s\t%s\t%s%s\n", s.Name, s.Mode, s.Source, missing)
	}
	w.Flush()
	fmt.Printf("\n%d skills. Auto ones cost about %d tokens of descriptions in every session; manual ones cost nothing\n"+
		"until you call them (/name in Claude Code, $name in Codex).\n", len(list), skills.TokenEstimate(list))
	return nil
}

type skillsAddCmd struct {
	From   string   `arg:"" placeholder:"OWNER/REPO" help:"The GitHub repo the skills come from, e.g. anthropics/skills."`
	Names  []string `arg:"" name:"skill" help:"Skill names in that repo."`
	Manual bool     `help:"Install them as manual: they run only when you call them."`
}

func (c skillsAddCmd) Run() error {
	catchUpLibrary()
	mode := skills.Auto
	if c.Manual {
		mode = skills.Manual
	}
	return reportSynced(skillManager(repoData()).Add(context.Background(), c.From, c.Names, mode))
}

type skillsRemoveCmd struct {
	Names []string `arg:"" name:"skill" help:"Skills to take off the list."`
}

func (c skillsRemoveCmd) Run() error {
	catchUpLibrary()
	return reportSynced(skillManager(repoData()).Remove(context.Background(), c.Names))
}

type skillsManualCmd struct {
	Names []string `arg:"" name:"skill"`
}

func (c skillsManualCmd) Run() error {
	catchUpLibrary()
	return skillManager(repoData()).SetMode(c.Names, skills.Manual)
}

type skillsAutoCmd struct {
	Names []string `arg:"" name:"skill"`
}

func (c skillsAutoCmd) Run() error {
	catchUpLibrary()
	return skillManager(repoData()).SetMode(c.Names, skills.Auto)
}

func syncSkills(ctx context.Context, repo source.Source) error {
	catchUpLibrary()
	return reportSynced(skillManager(repo).Sync(ctx))
}

// catchUpLibrary brings in a newer library from your other machines first, so a change here builds on it.
// Without a running service there is nothing to catch up with, and the library here is used as it is.
func catchUpLibrary() {
	cfg, err := config.Load()
	if err != nil {
		return
	}
	s, err := config.LoadSecrets(cfg)
	if err != nil {
		return
	}
	l := localService(cfg, "")
	l.client.APIKey = s.APIKey
	_ = l.catchUpLibrary()
}

func reportSynced(s skills.Synced, err error) error {
	if s.Count > 0 {
		fmt.Printf("%d skills for %s. Restart Claude Code and Codex to load them.\n", s.Count, strings.Join(s.Agents, ", "))
	}
	return err
}

func skillManager(repo source.Source) skills.Manager {
	return skills.New(repo, yourLibrary(repo), step)
}
