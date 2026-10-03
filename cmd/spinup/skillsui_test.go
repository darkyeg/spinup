package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/darkyeg/spinup/internal/library"
	"github.com/darkyeg/spinup/internal/skills"
	"github.com/darkyeg/spinup/internal/source"
	"github.com/darkyeg/spinup/internal/tui"
)

// sandbox gives the skills code a home of its own, so no test touches the real machine.
type sandbox struct {
	home, claude, codex, library string
	manager                      skills.Manager
}

func newSandbox(t *testing.T) sandbox {
	t.Helper()
	root := t.TempDir()
	box := sandbox{home: filepath.Join(root, "home"), claude: filepath.Join(root, "claude"), codex: filepath.Join(root, "codex")}
	for _, dir := range []string{box.home, box.claude, box.codex} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("USERPROFILE", box.home)
	t.Setenv("HOME", box.home)
	t.Setenv("CLAUDE_CONFIG_DIR", box.claude)
	t.Setenv("CODEX_HOME", box.codex)
	box.library = filepath.Join(root, "library")
	box.manager = skills.New(source.Builtin(), library.At(box.library), nil)
	return box
}

func writeSkill(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: "+name+"\ndescription: Test skill "+name+".\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rowIDs(tab tui.Tab) []string {
	var ids []string
	for _, row := range tab.Rows {
		ids = append(ids, row.ID)
	}
	return ids
}

func cell(t *testing.T, tab tui.Tab, id string, column int) string {
	t.Helper()
	for _, row := range tab.Rows {
		if row.ID == id {
			return row.Cells[column]
		}
	}
	t.Fatalf("no row %q in %s: %v", id, tab.Title, rowIDs(tab))
	return ""
}

func tabs(t *testing.T, box sandbox) (shared, local, removed tui.Tab) {
	t.Helper()
	all, err := skillTabs(box.manager)
	if err != nil {
		t.Fatal(err)
	}
	return all[0], all[1], all[2]
}

func act(t *testing.T, box sandbox, action string, input string, ids ...string) string {
	t.Helper()
	notice, err := doSkillAction(box.manager, tui.Intent{Action: action, IDs: ids, Input: input})
	if err != nil {
		t.Fatalf("%s %v: %v", action, ids, err)
	}
	return notice
}

func TestTheListsSeparateWhatSpinupSharesFromWhatExistsOnlyHere(t *testing.T) {
	box := newSandbox(t)
	if _, err := box.manager.Create(context.Background(), "mine", "Mine."); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, filepath.Join(box.home, ".agents", "skills"), "stray")
	writeSkill(t, filepath.Join(box.codex, "skills"), "hatch")

	shared, local, removed := tabs(t, box)

	if !slices.Equal(rowIDs(shared), []string{"mine"}) || cell(t, shared, "mine", 2) != "Claude, Codex" {
		t.Fatalf("shared = %v, loaded by %q", rowIDs(shared), cell(t, shared, "mine", 2))
	}
	if !slices.Equal(rowIDs(local), []string{"hatch", "stray"}) {
		t.Fatalf("only here = %v", rowIDs(local))
	}
	if got := cell(t, local, "stray", 1); got != "installed here, not shared" {
		t.Errorf("stray is %q", got)
	}
	if got := cell(t, local, "hatch", 1); got != "only in Codex" {
		t.Errorf("hatch is %q", got)
	}
	if len(removed.Rows) != 0 {
		t.Errorf("removed = %v", rowIDs(removed))
	}
}

func TestKeepingASkillMakesItYoursEverywhereAndLinksTheAgentToIt(t *testing.T) {
	box := newSandbox(t)
	writeSkill(t, filepath.Join(box.codex, "skills"), "hatch")
	writeSkill(t, filepath.Join(box.home, ".agents", "skills"), "stray")

	act(t, box, actAdopt, "", "hatch", "stray")

	shared, local, _ := tabs(t, box)
	if !slices.Equal(rowIDs(shared), []string{"hatch", "stray"}) || len(local.Rows) != 0 {
		t.Fatalf("shared %v, only here %v", rowIDs(shared), rowIDs(local))
	}
	for _, name := range []string{"hatch", "stray"} {
		if _, err := os.Stat(filepath.Join(box.library, "skills", name, "SKILL.md")); err != nil {
			t.Errorf("%s is not in your library: %v", name, err)
		}
	}
	linked, err := os.Lstat(filepath.Join(box.codex, "skills", "hatch"))
	if err != nil || linked.Mode()&(os.ModeSymlink|os.ModeIrregular) == 0 {
		t.Fatalf("Codex's own folder should now be a link into spinup's store: %v, %v", linked, err)
	}
}

func TestRemovingAndRestoringRoundTripsWithoutLosingTheSkill(t *testing.T) {
	box := newSandbox(t)
	if _, err := box.manager.Create(context.Background(), "mine", "Mine."); err != nil {
		t.Fatal(err)
	}

	act(t, box, actRemove, "", "mine")
	shared, _, removed := tabs(t, box)
	if len(shared.Rows) != 0 || !slices.Equal(rowIDs(removed), []string{"mine"}) {
		t.Fatalf("after remove: shared %v, removed %v", rowIDs(shared), rowIDs(removed))
	}
	for _, dir := range []string{box.claude, box.codex} {
		if _, err := os.Lstat(filepath.Join(dir, "skills", "mine")); !os.IsNotExist(err) {
			t.Errorf("%s still loads a removed skill: %v", dir, err)
		}
	}

	act(t, box, actRestore, "", "mine")
	shared, _, removed = tabs(t, box)
	if !slices.Equal(rowIDs(shared), []string{"mine"}) || len(removed.Rows) != 0 {
		t.Fatalf("after restore: shared %v, removed %v", rowIDs(shared), rowIDs(removed))
	}
}

func TestRemovingAnOnlyHereSkillParksItAndLeavesTheRestAlone(t *testing.T) {
	box := newSandbox(t)
	if _, err := box.manager.Create(context.Background(), "mine", "Mine."); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, filepath.Join(box.home, ".agents", "skills"), "stray")
	writeSkill(t, filepath.Join(box.codex, "skills"), "hatch")

	act(t, box, actRemove, "", "hatch")

	shared, local, removed := tabs(t, box)
	if !slices.Equal(rowIDs(shared), []string{"mine"}) || !slices.Equal(rowIDs(local), []string{"stray"}) {
		t.Fatalf("shared %v, only here %v; removing hatch must not touch the others", rowIDs(shared), rowIDs(local))
	}
	if !slices.Equal(rowIDs(removed), []string{"hatch"}) {
		t.Fatalf("removed = %v", rowIDs(removed))
	}
}

func TestSwitchingModeFlipsEachSkillToTheOtherOne(t *testing.T) {
	box := newSandbox(t)
	for _, name := range []string{"one", "two"} {
		if _, err := box.manager.Create(context.Background(), name, name); err != nil {
			t.Fatal(err)
		}
	}
	act(t, box, actMode, "", "one")
	shared, _, _ := tabs(t, box)
	if cell(t, shared, "one", 1) != "manual" || cell(t, shared, "two", 1) != "auto" {
		t.Fatalf("modes: one=%s two=%s", cell(t, shared, "one", 1), cell(t, shared, "two", 1))
	}

	act(t, box, actMode, "", "one", "two")
	shared, _, _ = tabs(t, box)
	if cell(t, shared, "one", 1) != "auto" || cell(t, shared, "two", 1) != "manual" {
		t.Fatalf("after flipping both: one=%s two=%s", cell(t, shared, "one", 1), cell(t, shared, "two", 1))
	}
}

func TestRenamingFixesANameClashAndTheBannerClearsWithIt(t *testing.T) {
	box := newSandbox(t)
	for _, folder := range []string{"review-a", "review-b"} {
		if _, err := box.manager.Create(context.Background(), folder, "x"); err != nil {
			t.Fatal(err)
		}
		act(t, box, actRename, "review", folder)
	}
	shared, _, _ := tabs(t, box)
	if got := cell(t, shared, "review-b", 4); !strings.Contains(got, "same name as review") {
		t.Fatalf("note = %q", got)
	}
	if !strings.Contains(skillBanner(box.manager), "press n on review-b") {
		t.Fatalf("banner = %q", skillBanner(box.manager))
	}

	act(t, box, actRename, "review-two", "review-b")

	if banner := skillBanner(box.manager); banner != "" {
		t.Fatalf("the clash is fixed but the banner says %q", banner)
	}
}

func TestAnUnknownActionIsAnErrorNotASilentNothing(t *testing.T) {
	box := newSandbox(t)
	if _, err := doSkillAction(box.manager, tui.Intent{Action: "explode", IDs: []string{"x"}}); err == nil {
		t.Fatal("an unknown action should be reported")
	}
	if _, err := doSkillAction(box.manager, tui.Intent{Action: actRename, IDs: []string{"a", "b"}, Input: "c"}); err == nil {
		t.Fatal("renaming two skills at once should be refused")
	}
}
