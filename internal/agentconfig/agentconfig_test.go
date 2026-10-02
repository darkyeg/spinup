package agentconfig

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/darkyeg/spinup/internal/atomicfile"
	"github.com/darkyeg/spinup/internal/source"
)

func TestMergeSettings(t *testing.T) {
	patch := `{"_comment": "skip", "env": {"B": "2", "_comment": "skip"}, "outputStyle": "Concise", "nested": {"x": {"y": 1}}}`
	cases := []struct {
		name, current, want string
		changed             bool
	}{
		{
			name:    "keeps order and unknown keys, nested objects merge",
			current: `{"hooks": {"a": 1}, "env": {"A": "1"}, "outputStyle": "Verbose", "nested": {"x": {"z": 2}}}`,
			want: `{
  "hooks": {
    "a": 1
  },
  "env": {
    "A": "1",
    "B": "2"
  },
  "outputStyle": "Concise",
  "nested": {
    "x": {
      "z": 2,
      "y": 1
    }
  }
}
`,
			changed: true,
		},
		{
			name:    "missing keys are appended after the user's",
			current: `{"z": [1, {"k": null}], "a": true}`,
			want: `{
  "z": [
    1,
    {
      "k": null
    }
  ],
  "a": true,
  "env": {
    "B": "2",
    "_comment": "skip"
  },
  "outputStyle": "Concise",
  "nested": {
    "x": {
      "y": 1
    }
  }
}
`,
			changed: true,
		},
		{
			name:    "already merged is unchanged",
			current: `{"outputStyle":"Concise","env":{"B":"2"},"nested":{"x":{"y":1}}}`,
			changed: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, changed, err := mergeSettings([]byte(c.current), []byte(patch))
			if err != nil {
				t.Fatal(err)
			}
			if changed != c.changed || (c.changed && got != c.want) {
				t.Fatalf("changed=%v want %v\n%s", changed, c.changed, got)
			}
		})
	}
}

func TestMergeSettingsRejectsNonObjects(t *testing.T) {
	if _, _, err := mergeSettings([]byte(`[1]`), []byte(`{}`)); err == nil {
		t.Fatal("expected an error")
	}
}

func TestSetKey(t *testing.T) {
	cases := []struct {
		name, text, table, key, value, want string
	}{
		{"replaces an existing key", "a = 1\n# note\nb = 2\n", "", "b", "3", "a = 1\n# note\nb = 3\n"},
		{"inserts a top-level key before the first table", "a = 1\n\n[t]\nx = 1\n", "", "b", "2", "a = 1\nb = 2\n\n[t]\nx = 1\n"},
		{"inserts before trailing blank lines of a table", "[t]\nx = 1\n\n\n[u]\ny = 1\n", "t", "z", "true", "[t]\nx = 1\nz = true\n\n\n[u]\ny = 1\n"},
		{"appends a missing table", "a = 1\n", "agents", "m", `"x"`, "a = 1\n\n[agents]\nm = \"x\"\n"},
		{"replaces only inside the table", "m = 1\n[t]\nm = 2\n", "t", "m", "9", "m = 1\n[t]\nm = 9\n"},
		{"empty file", "", "", "a", "1", "a = 1\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := setKey(c.text, c.table, c.key, c.value); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestLiteral(t *testing.T) {
	cases := []struct {
		value any
		want  string
	}{{true, "true"}, {int64(-4), "-4"}, {1.5, "1.5"}, {100000.0, "100000.0"}, {"a\"b", `"a\"b"`}}
	for _, c := range cases {
		got, err := literal(c.value)
		if err != nil || got != c.want {
			t.Errorf("literal(%v) = %q, %v; want %q", c.value, got, err, c.want)
		}
	}
	if _, err := literal([]int{1}); err == nil {
		t.Error("expected an error for a list")
	}
}

func TestParseWanted(t *testing.T) {
	got, err := parseWanted("# c\na = \"x\"\nb = 2\n\n[agents]\nc = true\nd = 0.5\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []tomlSetting{{"", "a", `"x"`}, {"", "b", "2"}, {"agents", "c", "true"}, {"agents", "d", "0.5"}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v", got)
	}
	for _, bad := range []string{"a = [1]\n", "[a.b]\n", "a = {x = 1}\n", `a = "x" # c`, "just words\n"} {
		if _, err := parseWanted(bad); err == nil {
			t.Errorf("expected an error for %q", bad)
		}
	}
}

func fixture(t *testing.T) (source.Source, Homes) {
	t.Helper()
	repo, home := t.TempDir(), t.TempDir()
	put(t, filepath.Join(repo, "agents/AGENTS.md"), "shared\n\n")
	put(t, filepath.Join(repo, "agents/claude/agents/Explore.md"), "explore\n")
	put(t, filepath.Join(repo, "agents/claude/settings.json"), `{"_comment": "x", "outputStyle": "Concise"}`)
	put(t, filepath.Join(repo, "agents/codex/config.toml"), "model = \"m\"\n\n[agents]\nlevel = \"low\"\n")
	return source.At(repo), Homes{Claude: filepath.Join(home, "claude"), Codex: filepath.Join(home, "codex")}
}

func put(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestInstallIsIdempotent(t *testing.T) {
	src, homes := fixture(t)
	first, err := Install(src, homes)
	if err != nil || len(first) != 5 {
		t.Fatalf("first install wrote %v, %v", first, err)
	}
	second, err := Install(src, homes)
	if err != nil || len(second) != 0 {
		t.Fatalf("second install wrote %v, %v", second, err)
	}
	if drifted, _ := Drifted(src, homes); len(drifted) != 0 {
		t.Fatalf("drifted after install: %v", drifted)
	}
}

func TestInstallKeepsUserSettingsAndBacksUpOnce(t *testing.T) {
	src, homes := fixture(t)
	settings := filepath.Join(homes.Claude, "settings.json")
	put(t, settings, `{"hooks": {}, "outputStyle": "Verbose"}`)
	if _, err := Install(src, homes); err != nil {
		t.Fatal(err)
	}
	if got := get(t, settings); got != "{\n  \"hooks\": {},\n  \"outputStyle\": \"Concise\"\n}\n" {
		t.Fatalf("settings: %q", got)
	}
	put(t, settings, `{"hooks": {}, "outputStyle": "Other"}`)
	if _, err := Install(src, homes); err != nil {
		t.Fatal(err)
	}
	if got := get(t, settings+atomicfile.BackupSuffix); got != `{"hooks": {}, "outputStyle": "Verbose"}` {
		t.Fatalf("backup was overwritten: %q", got)
	}
}

func TestInstallAppendsPrivateInstructionsAndKeepsCodexComments(t *testing.T) {
	src, homes := fixture(t)
	checkout, _ := src.Checkout()
	put(t, filepath.Join(checkout, source.PrivateDir, "AGENTS.md"), "mine\n")
	config := filepath.Join(homes.Codex, "config.toml")
	put(t, config, "# keep\nmodel = \"old\"\n\n[agents]\nlevel = \"high\"\n\n[other]\nz = 1\n")
	if _, err := Install(src, homes); err != nil {
		t.Fatal(err)
	}
	if got := get(t, filepath.Join(homes.Claude, "CLAUDE.md")); got != "shared\n\nmine\n" {
		t.Fatalf("instructions: %q", got)
	}
	want := "# keep\nmodel = \"m\"\n\n[agents]\nlevel = \"low\"\n\n[other]\nz = 1\n"
	if got := get(t, config); got != want {
		t.Fatalf("config: %q", got)
	}
}

func TestDriftedReportsEditedFiles(t *testing.T) {
	src, homes := fixture(t)
	if _, err := Install(src, homes); err != nil {
		t.Fatal(err)
	}
	instructions := filepath.Join(homes.Claude, "CLAUDE.md")
	put(t, instructions, "edited\n")
	drifted, err := Drifted(src, homes)
	if err != nil || !slices.Equal(drifted, []string{instructions}) {
		t.Fatalf("drifted = %v, %v", drifted, err)
	}
}

func TestRepoDataPlansOnAnEmptyMachine(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	files, err := Plan(source.At(root), Homes{Claude: filepath.Join(home, "c"), Codex: filepath.Join(home, "x")})
	if err != nil || len(files) < 5 {
		t.Fatalf("plan: %d files, %v", len(files), err)
	}
}

func TestRootKeyIsNeverInsertedInsideAMultiLineArray(t *testing.T) {
	current := "matrix = [\n  [1, 2],\n  [3, 4],\n]\nnames = [\n  \"[agents]\",\n]\n\n[agents]\nlevel = \"low\"\n"

	got, err := applyCodexSettings(current, "model = \"m\"\n[agents]\nlevel = \"high\"\n")
	if err != nil {
		t.Fatal(err)
	}

	want := "matrix = [\n  [1, 2],\n  [3, 4],\n]\nnames = [\n  \"[agents]\",\n]\nmodel = \"m\"\n\n[agents]\nlevel = \"high\"\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTopLevelSkipsLinesInsideBrackets(t *testing.T) {
	lines := []string{"a = [", "  [1],", "]", "[t]", "b = { x = [", "  1 ] }", "# [c]", "c = \"]\""}
	want := []bool{true, false, false, true, true, false, true, true}
	if got := topLevel(lines); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
