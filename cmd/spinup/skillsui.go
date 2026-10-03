package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/darkyeg/spinup/internal/skills"
	"github.com/darkyeg/spinup/internal/tui"
)

const (
	actRemove  = "remove"
	actMode    = "mode"
	actRename  = "rename"
	actAdopt   = "adopt"
	actRestore = "restore"
)

const restartHint = "Restart Claude Code and Codex to load the change."

// runSkillsUI opens the full-screen list of your skills. It shows who looks after each skill, so you see
// at once which ones spinup shares and which exist only here, and it does every change in place.
func runSkillsUI() error {
	catchUpLibrary()
	repo := repoData()
	manager := skills.New(repo, yourLibrary(repo), nil) // silent: log lines would tear the screen
	tabs, err := skillTabs(manager)
	if err != nil {
		return err
	}
	model := tui.NewModel("spinup skills", tabs)
	model.Banner = skillBanner(manager)
	return tui.Run(model, func(intent tui.Intent) ([]tui.Tab, string, error) {
		notice, err := doSkillAction(manager, intent)
		if err != nil {
			return nil, "", err
		}
		tabs, err := skillTabs(manager)
		model.Banner = skillBanner(manager)
		return tabs, notice, err
	})
}

func skillBanner(manager skills.Manager) string {
	status, err := manager.Status()
	if err != nil || len(status.Clashes) == 0 {
		return ""
	}
	clash := status.Clashes[0]
	return fmt.Sprintf("Two skills are both called %q: %s. Agents reach only one; press n on %s to rename it.",
		clash.Name, strings.Join(clash.Folders, ", "), strings.Join(clash.Fix(), " and "))
}

// skillTabs arranges the machine's skills into three lists: the ones spinup shares, the ones that exist
// only here, and the ones you removed.
func skillTabs(manager skills.Manager) ([]tui.Tab, error) {
	entries, err := manager.Inventory()
	if err != nil {
		return nil, err
	}
	status, err := manager.Status()
	if err != nil {
		return nil, err
	}
	removed, err := manager.Trash()
	if err != nil {
		return nil, err
	}
	var shared, local []skills.Entry
	for _, entry := range entries {
		if entry.State == skills.Shared {
			shared = append(shared, entry)
		} else {
			local = append(local, entry)
		}
	}
	return []tui.Tab{
		sharedTab(shared, status),
		localTab(local),
		removedTab(removed),
	}, nil
}

func sharedTab(entries []skills.Entry, status skills.Status) tui.Tab {
	renamable := map[string]string{}
	for _, clash := range status.Clashes {
		for _, folder := range clash.Fix() {
			renamable[folder] = clash.Name
		}
	}
	tab := tui.Tab{
		Title:   fmt.Sprintf("Shared %d", len(entries)),
		Columns: []string{"NAME", "MODE", "LOADED BY", "FROM"},
		Empty:   "Nothing shared yet. Start one with: spinup skills new <name>",
		Actions: []tui.Action{
			{Key: 'd', Name: actRemove, Label: "remove", Confirm: "Remove %d? It moves to Removed, never deleted."},
			{Key: 'm', Name: actMode, Label: "auto/manual"},
			{Key: 'n', Name: actRename, Label: "rename", Ask: "New name:"},
		},
	}
	hasNote := false
	for _, entry := range entries {
		row := tui.Row{ID: entry.Name, Cells: []string{entry.Name, entry.Mode.String(), loadedBy(entry), entry.Source}}
		if clashesWith, clash := renamable[entry.Name]; clash {
			row.Cells = append(row.Cells, "same name as "+clashesWith)
			row.Tone, hasNote = tui.Warn, true
		} else if len(entry.Agents) == 0 {
			row.Cells = append(row.Cells, "not installed here")
			row.Tone, hasNote = tui.Warn, true
		}
		tab.Rows = append(tab.Rows, row)
	}
	if hasNote {
		tab.Columns = append(tab.Columns, "NOTE")
	}
	return tab
}

func localTab(entries []skills.Entry) tui.Tab {
	tab := tui.Tab{
		Title:   fmt.Sprintf("Only here %d", len(entries)),
		Columns: []string{"NAME", "WHERE", "LOADED BY"},
		Empty:   "Everything an agent can load here is in your library.",
		Actions: []tui.Action{
			{Key: 'a', Name: actAdopt, Label: "keep everywhere"},
			{Key: 'd', Name: actRemove, Label: "remove", Confirm: "Remove %d from this machine? It moves to Removed, never deleted."},
		},
	}
	for _, entry := range entries {
		where := "installed here, not shared"
		if entry.State == skills.AgentOnly {
			where = "only in " + joinAgents(entry.Agents)
		}
		tab.Rows = append(tab.Rows, tui.Row{
			ID: entry.Name, Cells: []string{entry.Name, where, loadedBy(entry)}, Tone: tui.Warn,
		})
	}
	return tab
}

func removedTab(removed []skills.Parked) tui.Tab {
	tab := tui.Tab{
		Title:   fmt.Sprintf("Removed %d", len(removed)),
		Columns: []string{"NAME", "REMOVED"},
		Empty:   "Nothing removed. Skills you remove wait here, so you can always bring them back.",
		Actions: []tui.Action{{Key: 'r', Name: actRestore, Label: "restore"}},
	}
	for _, p := range removed {
		tab.Rows = append(tab.Rows, tui.Row{
			ID: p.Name, Cells: []string{p.Name, p.Removed.Format("2006-01-02 15:04")}, Tone: tui.Dim,
		})
	}
	return tab
}

func loadedBy(entry skills.Entry) string {
	if len(entry.Agents) == 0 {
		return "-"
	}
	return joinAgents(entry.Agents)
}

func joinAgents(names []string) string {
	shown := make([]string, 0, len(names))
	for _, name := range names {
		if display, known := map[string]string{"claude-code": "Claude", "codex": "Codex"}[name]; known {
			name = display
		}
		shown = append(shown, name)
	}
	return strings.Join(shown, ", ")
}

// doSkillAction does one thing the person asked for, with the same Manager calls the commands use.
func doSkillAction(manager skills.Manager, intent tui.Intent) (string, error) {
	ctx := context.Background()
	switch intent.Action {
	case actRemove:
		return removeSkills(ctx, manager, intent.IDs)
	case actMode:
		return switchModes(manager, intent.IDs)
	case actRename:
		if len(intent.IDs) != 1 {
			return "", fmt.Errorf("rename one skill at a time")
		}
		if _, err := manager.Rename(ctx, intent.IDs[0], intent.Input); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s is now called %s. %s", intent.IDs[0], intent.Input, restartHint), nil
	case actAdopt:
		if _, err := manager.Adopt(ctx, intent.IDs); err != nil {
			return "", err
		}
		return fmt.Sprintf("Kept %s: yours now, on every machine. %s", strings.Join(intent.IDs, ", "), restartHint), nil
	case actRestore:
		for _, id := range intent.IDs {
			if _, err := manager.Restore(ctx, id); err != nil {
				return "", err
			}
		}
		return fmt.Sprintf("Restored %s: yours again, on every machine. %s", strings.Join(intent.IDs, ", "), restartHint), nil
	}
	return "", fmt.Errorf("unknown action %q", intent.Action)
}

// removeSkills sends shared skills through Remove, which also unlists them everywhere, and any other
// skill straight to the parked folder; nothing is deleted either way.
func removeSkills(ctx context.Context, manager skills.Manager, ids []string) (string, error) {
	entries, err := manager.Inventory()
	if err != nil {
		return "", err
	}
	var shared []string
	for _, id := range ids {
		index := slices.IndexFunc(entries, func(e skills.Entry) bool { return e.Name == id })
		switch {
		case index < 0:
			return "", fmt.Errorf("%s is no longer here", id)
		case entries[index].State == skills.Shared:
			shared = append(shared, id)
		default:
			if err := manager.Discard(id); err != nil {
				return "", err
			}
		}
	}
	if len(shared) > 0 {
		if _, err := manager.Remove(ctx, shared); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("Removed %s. They wait in Removed. %s", strings.Join(ids, ", "), restartHint), nil
}

// switchModes flips each skill to the other mode: auto becomes manual and manual becomes auto.
func switchModes(manager skills.Manager, ids []string) (string, error) {
	entries, err := manager.Inventory()
	if err != nil {
		return "", err
	}
	var toManual, toAuto []string
	for _, id := range ids {
		index := slices.IndexFunc(entries, func(e skills.Entry) bool { return e.Name == id })
		switch {
		case index < 0:
			return "", fmt.Errorf("%s is no longer here", id)
		case entries[index].Mode == skills.Manual:
			toAuto = append(toAuto, id)
		default:
			toManual = append(toManual, id)
		}
	}
	for _, change := range []struct {
		mode  skills.Mode
		names []string
	}{{skills.Manual, toManual}, {skills.Auto, toAuto}} {
		if len(change.names) == 0 {
			continue
		}
		if err := manager.SetMode(change.names, change.mode); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("%d made manual, %d made auto. %s", len(toManual), len(toAuto), restartHint), nil
}
