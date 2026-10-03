package library

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func paths(files []File) []string {
	var out []string
	for _, f := range files {
		out = append(out, f.Path)
	}
	return out
}

func TestSnapshotSharesYoursAndFetchedOnly(t *testing.T) {
	lib := At(t.TempDir())
	put(t, lib.Path(SkillsList), "{}")
	put(t, lib.Path(Instructions), "notes")
	put(t, lib.Path("skills/mine/SKILL.md"), "mine")
	put(t, lib.Path("fetched/theirs/SKILL.md"), "theirs")
	put(t, lib.Path("skills/mine/.DS_Store"), "junk")
	put(t, lib.Path("other.txt"), "not shared")

	files, err := lib.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{Instructions, "fetched/theirs/SKILL.md", "skills/mine/SKILL.md", SkillsList}
	if got := paths(files); !reflect.DeepEqual(got, want) {
		t.Fatalf("shared %v, want %v", got, want)
	}
}

func TestAMissingLibraryIsEmpty(t *testing.T) {
	files, err := At(filepath.Join(t.TempDir(), "none")).Snapshot()
	if err != nil || len(files) != 0 || Fingerprint(files) != "" {
		t.Fatalf("files %v, err %v", files, err)
	}
}

func TestFingerprintIgnoresFetchedCopies(t *testing.T) {
	made := []File{{Path: SkillsList, Data: []byte("{}")}}
	withCopy := append(made, File{Path: "fetched/x/SKILL.md", Data: []byte("x")})
	if Fingerprint(made) != Fingerprint(withCopy) {
		t.Fatal("a fetched copy changed the fingerprint")
	}
	if Fingerprint(made) == Fingerprint([]File{{Path: SkillsList, Data: []byte("[]")}}) {
		t.Fatal("different lists have the same fingerprint")
	}
	reordered := []File{{Path: "skills/a/SKILL.md", Data: []byte("a")}, {Path: SkillsList, Data: []byte("{}")}}
	if Fingerprint(reordered) != Fingerprint([]File{reordered[1], reordered[0]}) {
		t.Fatal("the order files arrive in changed the fingerprint")
	}
	if Fingerprint(withCopy[1:]) != "" {
		t.Fatal("a library with only fetched copies should read as untouched")
	}
}

func TestTakeReplacesYoursAndAddsFetchedCopies(t *testing.T) {
	lib := At(t.TempDir())
	put(t, lib.Path(SkillsList), "old list")
	put(t, lib.Path("skills/gone/SKILL.md"), "removed there")
	put(t, lib.Path("fetched/kept/SKILL.md"), "only here")
	put(t, lib.Path("fetched/both/SKILL.md"), "old copy")
	put(t, lib.Path("fetched/both/extra.md"), "old extra")

	err := lib.Take([]File{
		{Path: SkillsList, Data: []byte("new list")},
		{Path: "skills/new/SKILL.md", Data: []byte("new")},
		{Path: "fetched/both/SKILL.md", Data: []byte("new copy")},
	})
	if err != nil {
		t.Fatal(err)
	}

	files, _ := lib.Snapshot()
	want := []string{"fetched/both/SKILL.md", "fetched/kept/SKILL.md", "skills/new/SKILL.md", SkillsList}
	if got := paths(files); !reflect.DeepEqual(got, want) {
		t.Fatalf("library holds %v, want %v", got, want)
	}
	if got := read(t, lib.Path(SkillsList)); got != "new list" {
		t.Fatalf("list = %q", got)
	}
	if _, err := os.Stat(lib.Path("skills/gone")); !os.IsNotExist(err) {
		t.Fatal("the removed skill's empty folder stayed")
	}
}

func TestTakeRefusesPathsOutsideTheLibrary(t *testing.T) {
	lib := At(t.TempDir())
	for _, p := range []string{"../escape", "/abs", `skills\..\..\x`, "C:/x", "other.txt", "skills/x/.hidden", ".", "skills/.git/config", "skills/con/SKILL.md", "skills/x /SKILL.md"} {
		if err := lib.Take([]File{{Path: p, Data: []byte("x")}}); err == nil {
			t.Errorf("took %q", p)
		}
	}
}

func TestTrackerDatesChangesAndKeepsATakenDate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stamp.json")
	tr, err := NewTracker(path)
	if err != nil {
		t.Fatal(err)
	}
	first := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	if s, _ := tr.Observe("", first, first); s != (Stamp{}) {
		t.Fatalf("an empty library was dated: %+v", s)
	}
	s, _ := tr.Observe("a", first, first)
	if s != (Stamp{first, "a"}) {
		t.Fatalf("first change = %+v", s)
	}
	if again, _ := tr.Observe("a", first, first.Add(time.Hour)); again != s {
		t.Fatalf("an unchanged library was dated again: %+v", again)
	}

	taken := Stamp{first.Add(-time.Hour), "b"}
	if err := tr.Took(taken); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewTracker(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := reloaded.Observe("b", first, first.Add(2*time.Hour)); got != taken {
		t.Fatalf("after a restart the taken library is dated %+v, want %+v", got, taken)
	}
}

func TestATrackerWithoutMemoryDatesTheLibraryByItsFiles(t *testing.T) {
	tr, err := NewTracker(filepath.Join(t.TempDir(), "stamp.json"))
	if err != nil {
		t.Fatal(err)
	}
	edited, now := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	if s, _ := tr.Observe("old", edited, now); !s.ChangedAt.Equal(edited) {
		t.Fatalf("a library found without memory is dated %v, want its files' %v", s.ChangedAt, edited)
	}
	if s, _ := tr.Observe("edited", edited, now); !s.ChangedAt.Equal(now) {
		t.Fatalf("a change seen later is dated %v, want now", s.ChangedAt)
	}
}

func TestAdoptKeepsTheOldFilesTimes(t *testing.T) {
	old, lib := t.TempDir(), At(t.TempDir())
	put(t, filepath.Join(old, SkillsDir, "mine", "SKILL.md"), "mine")
	then := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(old, SkillsDir, "mine", "SKILL.md"), then, then); err != nil {
		t.Fatal(err)
	}
	if _, err := lib.Adopt(old); err != nil {
		t.Fatal(err)
	}
	files, _ := lib.Snapshot()
	if got := LastModified(files); !got.Equal(then) {
		t.Fatalf("the adopted library looks changed at %v, want %v", got, then)
	}
}

func TestAHalfTakenLibraryReadsAsUntouchedUntilTheTakeFinishes(t *testing.T) {
	tr, err := NewTracker(filepath.Join(t.TempDir(), "stamp.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	incoming := Stamp{now.Add(-time.Hour), "theirs"}
	if err := tr.Taking(incoming); err != nil {
		t.Fatal(err)
	}
	if s, _ := tr.Observe("half written", now, now); s != (Stamp{}) {
		t.Fatalf("a half-taken library is dated %+v; it must lose to any other", s)
	}
	if err := tr.Took(incoming); err != nil {
		t.Fatal(err)
	}
	if s, _ := tr.Observe("theirs", now, now); s != incoming {
		t.Fatalf("after the take the library is dated %+v, want %+v", s, incoming)
	}
}

func TestAnUnreadableMemoryIsForgotten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stamp.json")
	put(t, path, "not json")
	tr, err := NewTracker(path)
	if err == nil || tr == nil {
		t.Fatalf("tracker %v, err %v; want a fresh tracker and the error", tr, err)
	}
	edited, now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if s, _ := tr.Observe("x", edited, now); !s.ChangedAt.Equal(edited) {
		t.Fatalf("dated %v, want the files' time", s.ChangedAt)
	}
}

func TestAddFetchedAddsOnlyMissingSkills(t *testing.T) {
	lib := At(t.TempDir())
	put(t, lib.Path("fetched/have/SKILL.md"), "mine")
	added, err := lib.AddFetched([]File{
		{Path: "fetched/have/SKILL.md", Data: []byte("theirs")},
		{Path: "fetched/new/SKILL.md", Data: []byte("new")},
		{Path: "fetched/new/ref.md", Data: []byte("ref")},
		{Path: SkillsList, Data: []byte("not a fetched copy")},
	})
	if err != nil || !reflect.DeepEqual(added, []string{"new"}) {
		t.Fatalf("added %v, %v", added, err)
	}
	if read(t, lib.Path("fetched/have/SKILL.md")) != "mine" || read(t, lib.Path("fetched/new/ref.md")) != "ref" {
		t.Fatal("wrong files after adding")
	}
	if _, err := os.Stat(lib.Path(SkillsList)); err == nil {
		t.Fatal("added something that isn't a fetched copy")
	}
}

func TestAStampFromTheFutureIsRefused(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if (Stamp{ChangedAt: now.Add(30 * time.Second)}).FromTheFuture(now) {
		t.Error("a small clock difference was refused")
	}
	if !(Stamp{ChangedAt: now.Add(time.Hour)}).FromTheFuture(now) {
		t.Error("a stamp an hour ahead was accepted")
	}
}
