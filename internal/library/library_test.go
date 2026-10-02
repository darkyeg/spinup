package library

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func put(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestAdoptCopiesAnOldPrivateRepoOnce(t *testing.T) {
	old, lib := t.TempDir(), At(filepath.Join(t.TempDir(), "library"))
	put(t, filepath.Join(old, SkillsList), `{"sources":{}}`)
	put(t, filepath.Join(old, SkillsDir, "mine", "SKILL.md"), "mine")
	put(t, filepath.Join(old, Instructions), "old notes")
	put(t, filepath.Join(old, "MACHINES.md"), "not ours to take")

	adopted, err := lib.Adopt(old)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{SkillsList, SkillsDir, Instructions}; !reflect.DeepEqual(adopted, want) {
		t.Fatalf("adopted %v, want %v", adopted, want)
	}
	if got := read(t, lib.Path("skills/mine/SKILL.md")); got != "mine" {
		t.Fatalf("own skill = %q", got)
	}
	if _, err := os.Stat(lib.Path("MACHINES.md")); err == nil {
		t.Fatal("copied a file the library doesn't use")
	}

	put(t, lib.Path(Instructions), "edited here")
	if adopted, err := lib.Adopt(old); err != nil || len(adopted) != 0 {
		t.Fatalf("second adopt copied %v (%v), want nothing", adopted, err)
	}
	if got := read(t, lib.Path(Instructions)); got != "edited here" {
		t.Fatalf("adopt overwrote the library: %q", got)
	}
}

func TestAdoptWithoutAnOldRepoDoesNothing(t *testing.T) {
	lib := At(t.TempDir())
	if adopted, err := lib.Adopt(filepath.Join(t.TempDir(), "missing")); err != nil || len(adopted) != 0 {
		t.Fatalf("adopted %v (%v)", adopted, err)
	}
}
