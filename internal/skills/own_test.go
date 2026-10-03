package skills

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestCreateWritesASkillYouCanCallAndInstallsIt(t *testing.T) {
	f := newFixture(t, sampleManifest)

	added, err := f.manager.Create(context.Background(), "deploy-notes", "How we deploy.")
	if err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(added.Path, skillFile))
	if err != nil {
		t.Fatal(err)
	}
	if head := parseFrontmatter(string(body)); head.name != "deploy-notes" || head.description != "How we deploy." {
		t.Fatalf("frontmatter = %+v", head)
	}
	for _, dir := range f.paths.agentSkills() {
		if !exists(filepath.Join(dir, "deploy-notes", skillFile)) {
			t.Fatalf("not installed for %s", dir)
		}
	}
}

func TestCreateRefusesToOverwriteASkillYouAlreadyHave(t *testing.T) {
	f := newFixture(t, sampleManifest)
	write(t, f.lib.Path("skills/mine/SKILL.md"), "yours")

	if _, err := f.manager.Create(context.Background(), "mine", ""); err == nil {
		t.Fatal("overwriting your own skill should fail")
	}
	if body, _ := os.ReadFile(f.lib.Path("skills/mine/SKILL.md")); string(body) != "yours" {
		t.Fatalf("your skill was overwritten: %q", body)
	}
}

func TestImportTakesAFolderAndAZipAndKeepsTheSkillsName(t *testing.T) {
	f := newFixture(t, sampleManifest)
	from := filepath.Join(t.TempDir(), "sent-over")
	write(t, filepath.Join(from, skillFile), "---\nname: sent-over\n---\n")
	write(t, filepath.Join(from, "references", "notes.md"), "detail")

	added, err := f.manager.Import(context.Background(), from)
	if err != nil {
		t.Fatal(err)
	}
	if added.Name != "sent-over" || !exists(filepath.Join(added.Path, "references", "notes.md")) {
		t.Fatalf("added = %+v", added)
	}
	for _, dir := range f.paths.agentSkills() {
		if !exists(filepath.Join(dir, "sent-over", skillFile)) {
			t.Fatalf("not installed for %s", dir)
		}
	}
}

func TestImportUnpacksAZipThatWrapsTheSkillInAFolder(t *testing.T) {
	f := newFixture(t, sampleManifest)
	archive := writeZip(t, map[string]string{
		"review-team/SKILL.md":           "---\nname: review-team\n---\n",
		"review-team/references/lens.md": "detail",
	})

	added, err := f.manager.Import(context.Background(), archive)
	if err != nil {
		t.Fatal(err)
	}
	if added.Name != "review-team" || !exists(filepath.Join(added.Path, "references", "lens.md")) {
		t.Fatalf("added = %+v", added)
	}
}

func TestImportReportsAZipThatIsNotASkill(t *testing.T) {
	f := newFixture(t, sampleManifest)
	archive := writeZip(t, map[string]string{"notes/readme.md": "nothing here"})

	_, err := f.manager.Import(context.Background(), archive)
	if err == nil || !strings.Contains(err.Error(), skillFile) {
		t.Fatalf("err = %v; it should name the missing %s", err, skillFile)
	}
}

func TestImportSaysWhenTheFolderNameIsNotTheNameYouCallItBy(t *testing.T) {
	f := newFixture(t, sampleManifest)
	from := filepath.Join(t.TempDir(), "review-team-v2-main")
	write(t, filepath.Join(from, skillFile), "---\nname: review-team-v2\n---\n")

	added, err := f.manager.Import(context.Background(), from)
	if err != nil || added.Renamed != "review-team-v2" {
		t.Fatalf("added = %+v, %v", added, err)
	}
}

func TestStatusReportsWhatASyncWouldChangeWithoutChangingAnything(t *testing.T) {
	f := newFixture(t, sampleManifest)
	write(t, filepath.Join(f.paths.store, "stray", skillFile), "x")

	status, err := f.manager.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.UpToDate() || len(status.Missing()) != 2 || len(status.Unlisted) != 1 {
		t.Fatalf("missing %v, unlisted %v", status.Missing(), status.Unlisted)
	}
	if !exists(filepath.Join(f.paths.store, "stray", skillFile)) || len(f.calls) != 0 {
		t.Fatalf("Status installed or parked something: calls %v", f.calls)
	}
}

func writeZip(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sent.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	archive := zip.NewWriter(file)
	for name, body := range files {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStatusFindsTwoSkillsAnsweringToOneName(t *testing.T) {
	f := newFixture(t, sampleManifest)
	write(t, filepath.Join(f.paths.store, "review-team", skillFile), "---\nname: review\n---\n")
	write(t, filepath.Join(f.paths.store, "review-team-v2", skillFile), "---\nname: review\n---\n")
	write(t, filepath.Join(f.paths.store, "alone", skillFile), "---\nname: alone\n---\n")

	status, err := f.manager.Status()
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Clashes) != 1 {
		t.Fatalf("clashes = %+v", status.Clashes)
	}
	clash := status.Clashes[0]
	if clash.Name != "review" || !reflect.DeepEqual(clash.Folders, []string{"review-team", "review-team-v2"}) {
		t.Fatalf("clash = %+v", clash)
	}
	if !reflect.DeepEqual(clash.Fix(), []string{"review-team-v2"}) {
		t.Fatalf("fix = %v; it should keep the first and rename the rest", clash.Fix())
	}
}

func TestAFolderWithoutAFrontmatterNameIsCalledByItsFolder(t *testing.T) {
	f := newFixture(t, sampleManifest)
	write(t, filepath.Join(f.paths.store, "plain", skillFile), "no frontmatter here")

	status, err := f.manager.Status()
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Clashes) != 0 {
		t.Fatalf("clashes = %+v", status.Clashes)
	}
}

func TestAddKeepsASkillThatArrivesInADifferentlyNamedFolder(t *testing.T) {
	f := newFixture(t, `{"sources":{"o/a":["review"]}}`)
	f.manager.install = func(_ context.Context, from string, _, names []string) (string, error) {
		f.calls = append(f.calls, from)
		write(t, filepath.Join(f.paths.store, "review-team-v2", skillFile), "---\nname: review\n---\n")
		return "", nil
	}

	synced, err := f.manager.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if !exists(f.lib.Path("fetched/review/SKILL.md")) {
		t.Fatal("the library kept no copy under the name on the list")
	}
	if !exists(filepath.Join(f.paths.store, "review", skillFile)) {
		t.Fatal("it was not settled under the name on the list")
	}
	if slices.Contains(synced.Parked, "review-team-v2") || exists(filepath.Join(f.paths.store, "review-team-v2", skillFile)) {
		t.Fatalf("the original folder should be gone, not parked: %v", synced.Parked)
	}
}

func TestAddSaysWhichSkillTheRepoDidNotHave(t *testing.T) {
	f := newFixture(t, `{"sources":{"o/a":["nope"]}}`)
	f.manager.install = func(_ context.Context, from string, _, names []string) (string, error) {
		return "", nil
	}

	_, err := f.manager.Sync(context.Background())
	if err == nil || !strings.Contains(err.Error(), "o/a") {
		t.Fatalf("err = %v; it should name the source that came up empty", err)
	}
}
