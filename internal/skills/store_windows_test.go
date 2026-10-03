package skills

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestParkingWaitsForAnOpenSkillFile(t *testing.T) {
	for _, aside := range []bool{false, true} {
		name := "unlisted"
		if aside {
			name = "existing Claude directory"
		}
		t.Run(name, func(t *testing.T) {
			p := newFixture(t, sampleManifest).paths
			dir := filepath.Join(p.store, "stray")
			if aside {
				dir = filepath.Join(p.claudeSkills(), "stray")
			}
			file := filepath.Join(dir, skillFile)
			write(t, file, "keep this skill")
			path, err := windows.UTF16PtrFromString(file)
			if err != nil {
				t.Fatal(err)
			}
			handle, err := windows.CreateFile(path, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(dir, dir+"-probe"); !errors.Is(err, fs.ErrPermission) {
				windows.CloseHandle(handle)
				t.Fatalf("open skill should prevent a directory rename: %v", err)
			}
			released := make(chan struct{})
			go func() {
				time.Sleep(100 * time.Millisecond)
				windows.CloseHandle(handle)
				close(released)
			}()
			defer func() { <-released }()
			if aside {
				err = p.parkAside(dir, "stray")
			} else {
				err = p.park([]string{"stray"})
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(p.parked, "stray", skillFile))
			if err != nil || string(data) != "keep this skill" {
				t.Fatalf("parked skill = %q, %v", data, err)
			}
			if exists(dir) {
				t.Fatal("the skill is still in its original directory")
			}
		})
	}
}

func TestAJunctionToTheRightPlaceIsLeftAlone(t *testing.T) {
	p := newFixture(t, sampleManifest).paths
	target := filepath.Join(p.store, "kept")
	write(t, filepath.Join(target, skillFile), "x")
	link := filepath.Join(p.claudeSkills(), "kept")
	if err := os.MkdirAll(p.claudeSkills(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := p.linkDir(link, target); err != nil {
		t.Fatal(err)
	}

	if !alreadyLinks(link, target) {
		at, err := os.Readlink(link)
		t.Fatalf("a junction spinup just made reads as %q (%v); it should be recognised", at, err)
	}
	if alreadyLinks(link, filepath.Join(p.store, "elsewhere")) {
		t.Fatal("a junction to somewhere else should not count as already linked")
	}
	if err := p.linkDir(link, target); err != nil {
		t.Fatalf("re-linking the same junction should do nothing: %v", err)
	}
}
