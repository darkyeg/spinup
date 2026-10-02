package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/darkyeg/spinup/internal/source"
)

const sampleManifest = `{"agents":["claude-code","codex"],"manual":["hand"],"sources":{"o/a":["keep","hand"]}}`

type fixture struct {
	manager Manager
	root    string
	paths   paths
	calls   []string
	fail    map[string]bool
}

func newFixture(t *testing.T, manifestText string) *fixture {
	t.Helper()
	root, home := t.TempDir(), t.TempDir()
	f := &fixture{root: root, fail: map[string]bool{}, paths: paths{
		store: filepath.Join(home, "skills"), parked: filepath.Join(home, "parked"), claude: filepath.Join(home, "claude"),
	}}
	write(t, filepath.Join(root, "skills", "skills.json"), manifestText)
	write(t, filepath.Join(root, "local", "placeholder"), "")
	f.manager = Manager{
		src:   source.At(root),
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
	write(t, filepath.Join(f.root, "skills", "local", "mine", "SKILL.md"), "---\ndescription: mine\n---\n")
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
	write(t, filepath.Join(f.root, "skills", "local", "mine", "SKILL.md"), "x")
	if _, err := f.manager.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(f.root, "skills", "local", "mine")); err != nil {
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

	_, err := f.manager.Remove(context.Background(), []string{"nope"}, Shared)

	if err == nil || len(f.calls) > 0 {
		t.Fatalf("err = %v, installs = %v", err, f.calls)
	}
	data, _ := os.ReadFile(filepath.Join(f.root, "skills", "skills.json"))
	if string(data) != sampleManifest {
		t.Fatalf("file changed: %s", data)
	}
}

func TestPrivateEditNeedsAPrivateRepo(t *testing.T) {
	f := newFixture(t, sampleManifest)
	if err := os.RemoveAll(filepath.Join(f.root, "local")); err != nil {
		t.Fatal(err)
	}

	if err := f.manager.SetMode([]string{"keep"}, Manual, Private); err == nil {
		t.Fatal("expected an error without local/")
	}
}

func TestSetModeWritesManifestAndAppliesModes(t *testing.T) {
	f := newFixture(t, sampleManifest)
	write(t, filepath.Join(f.paths.store, "keep", "SKILL.md"), "x")

	if err := f.manager.SetMode([]string{"keep"}, Manual, Shared); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(f.root, "skills", "skills.json"))
	if !strings.Contains(string(data), `"manual": ["hand", "keep"]`) {
		t.Fatalf("manifest = %s", data)
	}
	if !exists(filepath.Join(f.paths.store, "keep", "agents", "openai.yaml")) {
		t.Fatal("Codex policy missing")
	}
}

func TestAddEditsPrivateListAndSyncs(t *testing.T) {
	f := newFixture(t, sampleManifest)

	if _, err := f.manager.Add(context.Background(), "o/new", []string{"fresh"}, Manual, Private); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(f.root, "local", "skills.json"))
	if !strings.Contains(string(data), `"o/new": ["fresh"]`) || !reflect.DeepEqual(f.calls, []string{"o/a", "o/new"}) {
		t.Fatalf("private manifest = %s, installs = %v", data, f.calls)
	}
}
