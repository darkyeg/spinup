package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRenameResolvesAClashWithoutMovingAnything(t *testing.T) {
	f := newFixture(t, sampleManifest)
	write(t, f.lib.Path("skills/review-a/SKILL.md"), "---\nname: review\ndescription: A.\n---\nbody a")
	write(t, f.lib.Path("skills/review-b/SKILL.md"), "---\nname: review\ndescription: B.\n---\nbody b")
	if _, err := f.manager.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, err := f.manager.Status()
	if err != nil || len(before.Clashes) != 1 {
		t.Fatalf("setup: clashes = %+v, %v", before.Clashes, err)
	}

	if _, err := f.manager.Rename(context.Background(), "review-b", "review-two"); err != nil {
		t.Fatal(err)
	}

	after, err := f.manager.Status()
	if err != nil || len(after.Clashes) != 0 {
		t.Fatalf("clashes after rename = %+v, %v", after.Clashes, err)
	}
	body, _ := os.ReadFile(filepath.Join(f.paths.store, "review-b", skillFile))
	if head := parseFrontmatter(string(body)); head.name != "review-two" || head.description != "B." {
		t.Fatalf("installed copy = %+v; only the name should have changed", head)
	}
	if !exists(f.lib.Path("skills/review-b/SKILL.md")) || !exists(filepath.Join(f.paths.store, "review-a", skillFile)) {
		t.Fatal("a folder moved or went missing")
	}
}

func TestRenameRefusesASkillThatIsNotYours(t *testing.T) {
	f := newFixture(t, sampleManifest)
	write(t, filepath.Join(f.paths.store, "theirs", skillFile), "---\nname: theirs\n---\n")

	if _, err := f.manager.Rename(context.Background(), "theirs", "mine"); err == nil {
		t.Fatal("renaming a skill outside your library should fail")
	}
}

func TestRenameRefusesANameThatCannotBeAFolder(t *testing.T) {
	f := newFixture(t, sampleManifest)
	write(t, f.lib.Path("skills/mine/SKILL.md"), "---\nname: mine\n---\n")

	if _, err := f.manager.Rename(context.Background(), "mine", "../escape"); err == nil {
		t.Fatal("a path is not a skill name")
	}
}

func TestWithNameEditsAddsOrCreatesTheNameLine(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"replaces", "---\nname: old\ndescription: d\n---\nbody", "---\nname: new\ndescription: d\n---\nbody"},
		{"adds", "---\ndescription: d\n---\nbody", "---\nname: new\ndescription: d\n---\nbody"},
		{"creates", "just a body", "---\nname: new\n---\n\njust a body"},
		{"keeps windows newlines", "---\r\nname: old\r\n---\r\nbody", "---\r\nname: new\r\n---\r\nbody"},
		{"leaves the body alone", "---\nname: old\n---\nname: not frontmatter", "---\nname: new\n---\nname: not frontmatter"},
	} {
		if got := withName(c.in, "new"); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
