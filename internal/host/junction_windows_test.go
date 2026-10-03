package host

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinkDirMakesAJunctionWindowsAndGoBothUnderstand(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")

	if err := LinkDir(link, target); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(link, "SKILL.md"))
	if err != nil || string(body) != "hello" {
		t.Fatalf("reading through the junction = %q, %v", body, err)
	}
	at, err := os.Readlink(link)
	if err != nil || at != target {
		t.Fatalf("Readlink = %q, %v; want %q", at, err, target)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&(os.ModeSymlink|os.ModeIrregular) == 0 {
		t.Fatalf("Lstat mode = %v, %v; spinup must see it as a link, not a real directory", info.Mode(), err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatalf("removing the junction: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "SKILL.md")); err != nil {
		t.Fatalf("removing the junction took the target's files with it: %v", err)
	}
}

func TestLinkDirRefusesToReplaceSomethingThatIsThere(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "link")
	for _, dir := range []string{target, link} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := LinkDir(link, target); err == nil {
		t.Fatal("linking over an existing directory should fail, not swallow it")
	}
}
