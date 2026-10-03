package skills

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSkillsGoToTheAgentsThatAreHereWithoutBeingTold(t *testing.T) {
	f := newFixture(t, sampleManifest)
	if err := os.MkdirAll(f.paths.claude, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, f.lib.Path("skills/mine/SKILL.md"), "x")

	synced, err := f.manager.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(synced.Agents, []string{"claude-code"}) {
		t.Fatalf("agents = %v; only Claude Code has a home here", synced.Agents)
	}
	if !exists(filepath.Join(f.paths.claudeSkills(), "mine", skillFile)) {
		t.Fatal("Claude Code did not get the skill")
	}
	if _, err := os.Stat(filepath.Join(f.paths.codex, "skills")); !os.IsNotExist(err) {
		t.Fatal("spinup made a home for an agent that is not installed")
	}
}

func TestBeforeAnyAgentIsInstalledSpinupPreparesThemAll(t *testing.T) {
	f := newFixture(t, sampleManifest)
	write(t, f.lib.Path("skills/mine/SKILL.md"), "x")

	synced, err := f.manager.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(synced.Agents) != 2 {
		t.Fatalf("agents = %v; setup installs skills before the agents, so both are prepared", synced.Agents)
	}
}
