package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/darkyeg/spinup/internal/library"
	"github.com/darkyeg/spinup/internal/source"
)

const sampleManifest = `{"agents":["claude-code","codex"],"manual":["hand"],"sources":{"o/a":["keep","hand"]}}`

type fixture struct {
	manager Manager
	root    string
	lib     library.Library
	paths   paths
	calls   []string
	fail    map[string]bool
}

func newFixture(t *testing.T, manifestText string) *fixture {
	t.Helper()
	root, home := t.TempDir(), t.TempDir()
	f := &fixture{root: root, lib: library.At(filepath.Join(home, "library")), fail: map[string]bool{}, paths: paths{
		store: filepath.Join(home, "skills"), parked: filepath.Join(home, "parked"),
		claude: filepath.Join(home, "claude"), codex: filepath.Join(home, "codex"),
	}}
	write(t, filepath.Join(root, "skills", "skills.json"), manifestText)
	f.manager = Manager{
		src:   source.At(root),
		lib:   f.lib,
		paths: f.paths,
		install: func(_ context.Context, from string, _, names []string) (string, error) {
			f.calls = append(f.calls, from)
			if f.fail[from] {
				return "boom", errors.New("exit 1")
			}
			for _, name := range names {
				write(t, filepath.Join(f.paths.store, name, "SKILL.md"), "---\ndescription: installed\n---\n")
			}
			return "", nil
		},
		logf: func(string, ...any) {},
	}
	return f
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestParkable(t *testing.T) {
	tests := []struct {
		name      string
		installed []string
		failed    int
		want      []string
	}{
		{"parks unlisted, sorted", []string{"b", "keep", "a"}, 0, []string{"a", "b"}},
		{"parks nothing after a failed install", []string{"a"}, 1, nil},
		{"nothing unlisted", []string{"keep"}, 0, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parkable(tt.installed, []string{"keep"}, tt.failed); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSyncInstallsLinksParksAndAppliesModes(t *testing.T) {
	f := newFixture(t, sampleManifest)
	write(t, f.lib.Path("skills/mine/SKILL.md"), "---\ndescription: mine\n---\n")
	write(t, filepath.Join(f.paths.store, "stray", "SKILL.md"), "x")

	synced, err := f.manager.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if synced.Count != 3 || !reflect.DeepEqual(synced.Parked, []string{"stray"}) {
		t.Fatalf("synced = %+v", synced)
	}
	if !exists(filepath.Join(f.paths.parked, "stray", "SKILL.md")) || exists(filepath.Join(f.paths.store, "stray")) {
		t.Fatal("stray skill was not parked")
	}
	if !exists(filepath.Join(f.paths.claudeSkills(), "mine", "SKILL.md")) {
		t.Fatal("own skill is not linked into Claude")
	}
	if !exists(filepath.Join(f.paths.store, "hand", "agents", "openai.yaml")) {
		t.Fatal("manual skill has no Codex policy")
	}
	settings, _ := os.ReadFile(filepath.Join(f.paths.claude, "settings.json"))
	if !strings.Contains(string(settings), `"hand": "user-invocable-only"`) {
		t.Fatalf("settings = %s", settings)
	}
}

func TestSyncParksNothingWhenAnInstallFails(t *testing.T) {
	f := newFixture(t, sampleManifest)
	f.fail["o/a"] = true
	write(t, filepath.Join(f.paths.store, "stray", "SKILL.md"), "x")

	synced, err := f.manager.Sync(context.Background())

	if err == nil || !strings.Contains(err.Error(), "o/a") {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(synced.Failed, []string{"o/a"}) || !exists(filepath.Join(f.paths.store, "stray")) {
		t.Fatalf("synced = %+v", synced)
	}
}

func TestSyncRemovesDanglingClaudeLinks(t *testing.T) {
	f := newFixture(t, sampleManifest)
	write(t, f.lib.Path("skills/mine/SKILL.md"), "x")
	if _, err := f.manager.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(f.lib.Path("skills/mine")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(f.paths.store, "mine")); err != nil {
		t.Fatal(err)
	}

	if _, err := f.manager.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Lstat(filepath.Join(f.paths.claudeSkills(), "mine")); !os.IsNotExist(err) {
		t.Fatalf("dangling link remains, err = %v", err)
	}
}

func TestListReportsModeSourceAndInstalledState(t *testing.T) {
	f := newFixture(t, `{"agents":[],"manual":["hand"],"sources":{"o/a":["auto","hand","missing"]}}`)
	write(t, filepath.Join(f.paths.store, "auto", "SKILL.md"), "---\ndescription: twelve chars\n---\n")
	write(t, filepath.Join(f.paths.store, "hand", "SKILL.md"), "---\ndescription: ignored\n---\n")

	got, err := f.manager.List()
	if err != nil {
		t.Fatal(err)
	}

	want := []Skill{
		{"auto", "o/a", Auto, true, 12},
		{"hand", "o/a", Manual, true, 7},
		{"missing", "o/a", Auto, false, 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if TokenEstimate(got) != 3 {
		t.Fatalf("estimate = %d", TokenEstimate(got))
	}
}

func TestRemoveWithoutMatchLeavesTheFileAlone(t *testing.T) {
	f := newFixture(t, sampleManifest)

	_, err := f.manager.Remove(context.Background(), []string{"nope"})

	if err == nil || len(f.calls) > 0 {
		t.Fatalf("err = %v, installs = %v", err, f.calls)
	}
	if exists(f.lib.Path(library.SkillsList)) {
		t.Fatal("a failed edit started your list")
	}
}

func TestInvalidSkillEditsLeaveTheLibraryUsable(t *testing.T) {
	for _, name := range []string{"CON", "../outside"} {
		for _, existing := range []bool{false, true} {
			for _, command := range []string{"add", "manual"} {
				f := newFixture(t, sampleManifest)
				if existing {
					write(t, f.lib.Path(library.SkillsList), sampleManifest)
				}
				var err error
				if command == "add" {
					_, err = f.manager.Add(context.Background(), "owner/repo", []string{name}, Auto)
				} else {
					err = f.manager.SetMode([]string{name}, Manual)
				}
				if err == nil {
					t.Fatalf("%s accepted %q", command, name)
				}
				data, readErr := os.ReadFile(f.lib.Path(library.SkillsList))
				if existing && (readErr != nil || string(data) != sampleManifest) || !existing && !os.IsNotExist(readErr) {
					t.Fatalf("%s with %q changed the library: %s, %v", command, name, data, readErr)
				}
				if len(f.calls) != 0 {
					t.Fatal("an invalid edit started installing skills")
				}
			}
		}
	}
}

func TestTheFirstEditStartsYourListFromSpinupsAndLeavesSpinupsAlone(t *testing.T) {
	suggested := `{"_comment":"spinup's","agents":["codex"],"manual":["hand"],"sources":{"o/a":["keep","hand"]}}`
	f := newFixture(t, suggested)
	write(t, filepath.Join(f.paths.store, "keep", "SKILL.md"), "x")

	if err := f.manager.SetMode([]string{"keep"}, Manual); err != nil {
		t.Fatal(err)
	}

	yours, _ := os.ReadFile(f.lib.Path(library.SkillsList))
	for _, part := range []string{`"agents": ["codex"]`, `"o/a": ["keep", "hand"]`, `"manual": ["hand", "keep"]`} {
		if !strings.Contains(string(yours), part) {
			t.Fatalf("your list lacks %s:\n%s", part, yours)
		}
	}
	if strings.Contains(string(yours), "_comment") {
		t.Fatalf("your list kept spinup's comment:\n%s", yours)
	}
	spinups, _ := os.ReadFile(filepath.Join(f.root, "skills", "skills.json"))
	if string(spinups) != suggested {
		t.Fatalf("spinup's list changed: %s", spinups)
	}
}

func TestSetModeWritesYourListAndAppliesModes(t *testing.T) {
	f := newFixture(t, sampleManifest)
	write(t, filepath.Join(f.paths.store, "keep", "SKILL.md"), "x")

	if err := f.manager.SetMode([]string{"keep"}, Manual); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(f.lib.Path(library.SkillsList))
	if !strings.Contains(string(data), `"manual": ["hand", "keep"]`) {
		t.Fatalf("your list = %s", data)
	}
	if !exists(filepath.Join(f.paths.store, "keep", "agents", "openai.yaml")) {
		t.Fatal("Codex policy missing")
	}
}

func TestAddEditsYourListAndSyncs(t *testing.T) {
	f := newFixture(t, sampleManifest)

	if _, err := f.manager.Add(context.Background(), "o/new", []string{"fresh"}, Manual); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(f.lib.Path(library.SkillsList))
	if !strings.Contains(string(data), `"o/new": ["fresh"]`) || !reflect.DeepEqual(f.calls, []string{"o/a", "o/new"}) {
		t.Fatalf("your list = %s, installs = %v", data, f.calls)
	}
}

func TestOnceYourListExistsSpinupsIsNotUsed(t *testing.T) {
	f := newFixture(t, sampleManifest)
	write(t, f.lib.Path(library.SkillsList), `{"agents":["codex"],"sources":{"o/yours":["only"]}}`)

	if _, err := f.manager.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(f.calls, []string{"o/yours"}) {
		t.Fatalf("installed from %v, want only your list's source", f.calls)
	}
}

func TestWithTheBuiltInDataYourOwnSkillsStayAndStraysAreParked(t *testing.T) {
	f := newFixture(t, sampleManifest)
	f.manager.src = source.Builtin()
	write(t, f.lib.Path("skills/mine/SKILL.md"), "x")
	write(t, filepath.Join(f.paths.store, "stray", "SKILL.md"), "x")

	synced, err := f.manager.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(synced.Parked, []string{"stray"}) || !exists(filepath.Join(f.paths.store, "mine", "SKILL.md")) {
		t.Fatalf("parked %v; your own skill installed: %v", synced.Parked, exists(filepath.Join(f.paths.store, "mine")))
	}
}

func TestUnlistedCountsLinkedSkillDirsLikeSyncDoes(t *testing.T) {
	f := newFixture(t, sampleManifest)
	elsewhere := filepath.Join(t.TempDir(), "linked")
	write(t, filepath.Join(elsewhere, "SKILL.md"), "x")
	if err := os.MkdirAll(f.paths.store, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(f.paths.store, "linked")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}

	unlisted, err := f.manager.Unlisted()
	if err != nil || !reflect.DeepEqual(unlisted, []string{"linked"}) {
		t.Fatalf("Unlisted = %v, %v", unlisted, err)
	}
	synced, err := f.manager.Sync(context.Background())
	if err != nil || !reflect.DeepEqual(synced.Parked, unlisted) {
		t.Fatalf("Sync parked %v, %v", synced.Parked, err)
	}
}

func TestPublishParksARealDirectoryInsteadOfDeletingIt(t *testing.T) {
	f := newFixture(t, sampleManifest)
	write(t, f.lib.Path("skills/mine/SKILL.md"), "ours")
	write(t, filepath.Join(f.paths.claudeSkills(), "mine", "notes.txt"), "the user's")

	if _, err := f.manager.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !exists(filepath.Join(f.paths.parked, "mine", "notes.txt")) {
		t.Fatal("the user's directory was not kept in the parked folder")
	}
	if !exists(filepath.Join(f.paths.claudeSkills(), "mine", "SKILL.md")) {
		t.Fatal("own skill is not linked")
	}
}

func TestSyncFetchesOnceAndKeepsEveryCopyInYourLibrary(t *testing.T) {
	f := newFixture(t, sampleManifest)
	write(t, f.lib.Path("fetched/dropped/SKILL.md"), "no longer listed")

	if _, err := f.manager.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(f.calls, []string{"o/a"}) {
		t.Fatalf("fetched %v, want o/a once", f.calls)
	}
	for _, name := range []string{"keep", "hand"} {
		if !exists(f.lib.Path("fetched/" + name + "/SKILL.md")) {
			t.Errorf("no copy of %s in the library", name)
		}
	}
	if !exists(f.lib.Path("fetched/dropped/SKILL.md")) {
		t.Error("a copy of a skill no longer listed was deleted; a partner would only send it back")
	}
}

func TestOfflineInstallsTheLibrarysCopiesWithoutFetching(t *testing.T) {
	f := newFixture(t, sampleManifest)
	write(t, f.lib.Path("fetched/keep/SKILL.md"), "---\ndescription: from another machine\n---\n")
	write(t, f.lib.Path("fetched/hand/SKILL.md"), "---\ndescription: from another machine\n---\n")

	if _, err := f.manager.Offline().Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(f.calls) > 0 {
		t.Fatalf("fetched %v while offline", f.calls)
	}
	if !exists(filepath.Join(f.paths.store, "keep", "SKILL.md")) || !exists(filepath.Join(f.paths.claudeSkills(), "keep", "SKILL.md")) {
		t.Fatal("the library's copy wasn't installed for both agents")
	}
}

func TestOfflineSaysWhatTheLibraryLacksAndParksNothing(t *testing.T) {
	f := newFixture(t, sampleManifest)
	write(t, filepath.Join(f.paths.store, "stray", "SKILL.md"), "x")

	_, err := f.manager.Offline().Sync(context.Background())

	if err == nil || len(f.calls) > 0 {
		t.Fatalf("err = %v, fetched %v", err, f.calls)
	}
	if !exists(filepath.Join(f.paths.store, "stray")) {
		t.Fatal("parked a skill while the list couldn't be installed")
	}
}

func TestCodexGetsEverySkillClaudeCodeGetsEvenWhenTheStoreAlreadyHadIt(t *testing.T) {
	f := newFixture(t, sampleManifest)
	write(t, f.lib.Path("fetched/keep/SKILL.md"), "---\ndescription: cached\n---\n")
	write(t, f.lib.Path("fetched/hand/SKILL.md"), "---\ndescription: cached\n---\n")
	write(t, filepath.Join(f.paths.store, "keep", "SKILL.md"), "---\ndescription: already here\n---\n")

	if _, err := f.manager.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(f.calls) != 0 {
		t.Fatalf("a cached skill should not be fetched again: %v", f.calls)
	}
	for _, name := range []string{"keep", "hand"} {
		for _, dir := range f.paths.agentSkills() {
			if !exists(filepath.Join(dir, name, "SKILL.md")) {
				t.Fatalf("%s is not linked into %s", name, dir)
			}
		}
	}
}

func TestParkingRemovesTheSkillFromEveryAgentNotJustClaudeCode(t *testing.T) {
	f := newFixture(t, sampleManifest)
	write(t, f.lib.Path("skills/mine/SKILL.md"), "x")
	if _, err := f.manager.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(f.lib.Path("skills/mine")); err != nil {
		t.Fatal(err)
	}

	synced, err := f.manager.Sync(context.Background())
	if err != nil || !slices.Contains(synced.Parked, "mine") {
		t.Fatalf("parked %v, %v", synced.Parked, err)
	}
	for _, dir := range f.paths.agentSkills() {
		if _, err := os.Lstat(filepath.Join(dir, "mine")); !os.IsNotExist(err) {
			t.Fatalf("%s still links a parked skill: %v", dir, err)
		}
	}
}
