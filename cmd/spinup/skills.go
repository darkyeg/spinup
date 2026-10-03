package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/skills"
	"github.com/darkyeg/spinup/internal/source"
)

type skillsCmd struct {
	List    skillsListCmd    `cmd:"" default:"1" help:"Your skills, who loads them, and which exist only here. Opens an interactive list in a terminal."`
	Sync    skillsSyncCmd    `cmd:"" help:"Install what is missing and park what is not on your list."`
	New     skillsNewCmd     `cmd:"" help:"Start a skill of your own: writes a SKILL.md in your library and installs it."`
	Import  skillsImportCmd  `cmd:"" help:"Take a skill someone sent you (a folder or a .zip) into your library."`
	Adopt   skillsAdoptCmd   `cmd:"" help:"Take skills installed only on this machine into your library, so every machine gets them."`
	Add     skillsAddCmd     `cmd:"" help:"Add skills from a GitHub repo to your list and install them."`
	Remove  skillsRemoveCmd  `cmd:"" help:"Remove skills. They move to the parked folder, never deleted; see spinup skills trash."`
	Manual  skillsManualCmd  `cmd:"" help:"Make a skill run only when you call it (/name, $name), so it costs no tokens until then."`
	Auto    skillsAutoCmd    `cmd:"" help:"Let the agent reach for a skill by itself again."`
	Trash   skillsTrashCmd   `cmd:"" help:"Every removed skill, newest first, and how to bring one back."`
	Restore skillsRestoreCmd `cmd:"" help:"Bring a removed skill back, into your library and onto every machine."`
}

func (skillsCmd) Help() string {
	return `Your skills list and your own skills live in your library, ~/.spinup. Until you change the
list, spinup's suggested one is used; your first change copies it there.

  spinup skills                      your skills in an interactive list: remove, restore, keep, rename
  spinup skills sync                 install what is missing, park what is not on the list
  spinup skills new my-skill         start a skill of your own
  spinup skills import ./some-skill  take a skill someone sent you (folder or .zip)
  spinup skills adopt                take this machine's own skills into your library

Every command that takes skill names runs without them too, and asks you to pick:

  spinup skills remove               pick what to remove
  spinup skills manual               pick what should stop running on its own
  spinup skills auto                 pick what may run on its own again

Nothing is ever deleted. Removed skills wait in the parked folder:

  spinup skills trash                what you removed, and when
  spinup skills restore my-skill     bring one back`
}

type skillsSyncCmd struct{}

func (skillsSyncCmd) Run() error { return syncSkills(context.Background(), repoData()) }

type skillsListCmd struct {
	Plain bool `help:"Print the list instead of opening the interactive one."`
}

func (c skillsListCmd) Run() error {
	if !c.Plain && interactive() {
		return runSkillsUI()
	}
	status, err := skillManager(repoData()).Status()
	if err != nil {
		return err
	}
	printSkills(status)
	return nil
}

func printSkills(status skills.Status) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, s := range status.Skills {
		missing := ""
		if !s.Installed {
			missing = "  (not installed)"
		}
		fmt.Fprintf(w, "  %s\t%s\t%s%s\n", s.Name, s.Mode, s.Source, missing)
	}
	w.Flush()
	fmt.Printf("\n%d skills in %s. Auto ones cost about %d tokens of descriptions in every session;\n"+
		"manual ones cost nothing until you call them (/name in Claude Code, $name in Codex).\n",
		len(status.Skills), status.Library, skills.TokenEstimate(status.Skills))
	printPending(status)
}

// printPending says what `spinup skills sync` would change, so the default command never surprises anyone.
func printPending(status skills.Status) {
	printClashes(status.Clashes)
	if len(status.AgentOnly) > 0 {
		fmt.Printf("\nOnly in one agent, never shared (%d): %s\n", len(status.AgentOnly), strings.Join(status.AgentOnly, ", "))
		fmt.Println("  Open `spinup skills` in a terminal to keep them on every machine, or remove them.")
	}
	if status.UpToDate() {
		fmt.Println("\nEverything on your list is installed. Nothing to do.")
		return
	}
	if missing := status.Missing(); len(missing) > 0 {
		fmt.Printf("\nNot installed here (%d): %s\n", len(missing), strings.Join(missing, ", "))
		fmt.Println("  Run `spinup skills sync` to install them.")
	}
	if len(status.Unlisted) > 0 {
		fmt.Printf("\nInstalled here but not in your library (%d): %s\n",
			len(status.Unlisted), strings.Join(status.Unlisted, ", "))
		fmt.Println("  A sync removes these, and your other machines never see them.")
		fmt.Println("  Run `spinup skills adopt` to keep them: they become yours, on every machine.")
	}
}

// printClashes names the skills that answer to one name, because only one of them can be reached.
func printClashes(clashes []skills.Clash) {
	for _, clash := range clashes {
		fmt.Printf("\nTwo skills are both called %q: %s\n", clash.Name, strings.Join(clash.Folders, ", "))
		fmt.Printf("  Agents reach only one of them. Rename %s, or take it off your list.\n",
			strings.Join(clash.Fix(), " and "))
	}
}

type skillsNewCmd struct {
	Name        string `arg:"" help:"The skill's folder name, e.g. deploy-notes."`
	Description string `help:"One line telling the agent when to use it; you can fill it in later."`
}

func (c skillsNewCmd) Run() error {
	catchUpLibrary()
	added, err := skillManager(repoData()).Create(context.Background(), c.Name, c.Description)
	return reportAdded(added, err, "Edit it, then run `spinup skills sync` to share the change.")
}

type skillsImportCmd struct {
	From string `arg:"" placeholder:"PATH" help:"A skill folder with a SKILL.md, or a .zip holding one."`
}

func (c skillsImportCmd) Run() error {
	catchUpLibrary()
	added, err := skillManager(repoData()).Import(context.Background(), c.From)
	return reportAdded(added, err, "It is yours now: it reaches your other machines by itself.")
}

func reportAdded(added skills.Added, err error, next string) error {
	if err != nil {
		return err
	}
	fmt.Printf("%s is in your library: %s\n", added.Name, added.Path)
	if added.Renamed != "" {
		fmt.Printf("Its SKILL.md calls itself %q, so that is the name you call it by.\n", added.Renamed)
	}
	fmt.Println(next)
	return reportSynced(added.Synced, nil)
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
	Names []string `arg:"" optional:"" name:"skill" help:"Skills to take off the list; without any, you pick them from a list."`
}

func (c skillsRemoveCmd) Run() error {
	catchUpLibrary()
	manager := skillManager(repoData())
	names := c.Names
	if len(names) == 0 {
		chosen, err := chooseSkills(manager)
		if err != nil {
			return err
		}
		names = chosen
	}
	return reportSynced(manager.Remove(context.Background(), names))
}

// chooseSkills asks which skills to take off the list, for `spinup skills remove` without names.
func chooseSkills(manager skills.Manager) ([]string, error) {
	status, err := manager.Status()
	if err != nil {
		return nil, err
	}
	if len(status.Skills) == 0 {
		return nil, fmt.Errorf("your list is empty, so there is nothing to remove")
	}
	var names []string
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for i, s := range status.Skills {
		fmt.Fprintf(w, "  %2d\t%s\t%s\t%s\n", i+1, s.Name, s.Mode, s.Source)
		names = append(names, s.Name)
	}
	w.Flush()
	fmt.Println("\nThey are parked, not deleted; `spinup skills add` brings one back.")
	chosen, err := pickNames(askLine("Which to remove? (numbers, e.g. 2 5-7; empty cancels) "), names)
	if err != nil {
		return nil, err
	}
	if !confirm(fmt.Sprintf("Take %s off the list on all your machines?", strings.Join(chosen, ", "))) {
		return nil, errCancelled
	}
	return chosen, nil
}

type skillsManualCmd struct {
	Names []string `arg:"" optional:"" name:"skill" help:"Without any, you pick them from a list."`
}

func (c skillsManualCmd) Run() error { return setMode(c.Names, skills.Manual) }

type skillsAutoCmd struct {
	Names []string `arg:"" optional:"" name:"skill" help:"Without any, you pick them from a list."`
}

func (c skillsAutoCmd) Run() error { return setMode(c.Names, skills.Auto) }

func setMode(names []string, mode skills.Mode) error {
	catchUpLibrary()
	manager := skillManager(repoData())
	if len(names) == 0 {
		chosen, err := chooseMode(manager, mode)
		if err != nil {
			return err
		}
		names = chosen
	}
	return manager.SetMode(names, mode)
}

// chooseMode asks which skills to switch, offering only the ones not already in that mode.
func chooseMode(manager skills.Manager, mode skills.Mode) ([]string, error) {
	status, err := manager.Status()
	if err != nil {
		return nil, err
	}
	var names []string
	var switchable []skills.Skill
	for _, s := range status.Skills {
		if s.Mode != mode {
			names = append(names, s.Name)
			switchable = append(switchable, s)
		}
	}
	if len(names) == 0 {
		fmt.Printf("Every skill is already %s. Nothing to do.\n", mode)
		return nil, errCancelled
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for i, s := range switchable {
		fmt.Fprintf(w, "  %2d\t%s\t%s\t%d chars\n", i+1, s.Name, s.Mode, s.DescriptionChars)
	}
	w.Flush()
	if mode == skills.Manual {
		fmt.Println("\nThe chars are what a description costs in every session. Manual skills cost nothing\n" +
			"until you call them (/name, $name).")
	} else {
		fmt.Println("\nAuto skills can be used by the agent on its own, and their description costs those\n" +
			"chars in every session.")
	}
	return pickNames(askLine(fmt.Sprintf("Which to make %s? (numbers, e.g. 2 5-7; empty cancels) ", mode)), names)
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
	if len(s.Parked) > 0 {
		fmt.Printf("Parked %d: %s. They are kept, not deleted.\n", len(s.Parked), strings.Join(s.Parked, ", "))
	}
	return err
}

func skillManager(repo source.Source) skills.Manager {
	return skills.New(repo, yourLibrary(repo), step)
}

type skillsTrashCmd struct{}

func (skillsTrashCmd) Run() error {
	parked, err := skillManager(repoData()).Trash()
	if err != nil {
		return err
	}
	if len(parked) == 0 {
		fmt.Println("Nothing removed. Skills you remove wait here, so you can always bring them back.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, p := range parked {
		fmt.Fprintf(w, "  %s\t%s\n", p.Name, p.Removed.Format("2006-01-02 15:04"))
	}
	w.Flush()
	fmt.Printf("\n%d removed skills in %s.\n", len(parked), filepath.Dir(parked[0].Path))
	fmt.Printf("Bring one back with `spinup skills restore %s`.\n", parked[0].Name)
	return nil
}

type skillsRestoreCmd struct {
	Name string `arg:"" help:"The name as spinup skills trash lists it."`
}

func (c skillsRestoreCmd) Run() error {
	catchUpLibrary()
	added, err := skillManager(repoData()).Restore(context.Background(), c.Name)
	return reportAdded(added, err, "It is yours now: it reaches your other machines by itself.")
}

type skillsAdoptCmd struct {
	Names []string `arg:"" optional:"" name:"skill" help:"Without any, you pick from the skills only this machine has."`
}

func (c skillsAdoptCmd) Run() error {
	catchUpLibrary()
	manager := skillManager(repoData())
	names := c.Names
	if len(names) == 0 {
		chosen, err := chooseAdoptable(manager)
		if err != nil {
			return err
		}
		names = chosen
	}
	return reportSynced(manager.Adopt(context.Background(), names))
}

// chooseAdoptable asks which of this machine's own skills to take into the library, where they last.
func chooseAdoptable(manager skills.Manager) ([]string, error) {
	status, err := manager.Status()
	if err != nil {
		return nil, err
	}
	if len(status.Unlisted) == 0 {
		fmt.Println("Every installed skill is already in your library. Nothing to adopt.")
		return nil, errCancelled
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for i, name := range status.Unlisted {
		fmt.Fprintf(w, "  %2d\t%s\t%s\n", i+1, name, filepath.Join(status.Store, name))
	}
	w.Flush()
	fmt.Printf("\nThese %d are installed on this machine only. A sync parks them, and your other\n"+
		"machines never see them. Adopting one puts it in your library for good.\n", len(status.Unlisted))
	return pickNames(askLine("Which to adopt? (numbers, e.g. 2 5-7; empty cancels) "), status.Unlisted)
}
