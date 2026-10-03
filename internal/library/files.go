package library

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/darkyeg/spinup/internal/atomicfile"
)

// SizeLimit bounds a library sent between machines.
const SizeLimit = 64 << 20

// skillFile marks a folder as a skill.
const skillFile = "SKILL.md"

// File is one file of a library, as it travels between machines.
type File struct {
	// Path is relative to the library, with slashes.
	Path string `json:"path"`
	Data []byte `json:"data"`
	// modified is when the file last changed on this machine; it doesn't travel.
	modified time.Time
}

// Snapshot reads every file the library shares, folder by folder. Hidden files and links stay behind.
func (l Library) Snapshot() ([]File, error) {
	var files []File
	total := 0
	err := fs.WalkDir(l.Files(), ".", func(p string, d fs.DirEntry, err error) error {
		switch {
		case p == "." && errors.Is(err, fs.ErrNotExist):
			return fs.SkipAll
		case err != nil:
			return err
		case p == ".":
			return nil
		case strings.HasPrefix(d.Name(), ".") || !shared(p):
			return skip(d)
		case !d.Type().IsRegular():
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if total += int(info.Size()); total > SizeLimit {
			return fmt.Errorf("%s holds more than %d MB, too much to share", l.dir, SizeLimit>>20)
		}
		data, err := fs.ReadFile(l.Files(), p)
		if err != nil {
			return err
		}
		files = append(files, File{Path: p, Data: data, modified: info.ModTime()})
		return nil
	})
	return files, err
}

func skip(d fs.DirEntry) error {
	if d.IsDir() {
		return fs.SkipDir
	}
	return nil
}

// Fingerprint identifies what you made in a library (not the fetched copies); it is "" when there is nothing.
func Fingerprint(files []File) string {
	sorted := slices.SortedFunc(slices.Values(files), func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	h := sha256.New()
	made := 0
	for _, f := range sorted {
		if !yours(f.Path) {
			continue
		}
		made++
		fmt.Fprintf(h, "%s\x00%d\x00", f.Path, len(f.Data))
		h.Write(f.Data)
	}
	if made == 0 {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// LastModified is when what you made in files last changed on this machine.
func LastModified(files []File) time.Time {
	var latest time.Time
	for _, f := range files {
		if yours(f.Path) && f.modified.After(latest) {
			latest = f.modified
		}
	}
	return latest
}

// FetchedSkills names the skills files hold a fetched copy of.
func FetchedSkills(files []File) []string {
	var names []string
	for _, f := range files {
		if top, skill := split(f.Path); top == Fetched && f.Path == Fetched+"/"+skill+"/"+skillFile {
			names = append(names, skill)
		}
	}
	return names
}

// AddFetched adds the fetched copies in files that this library lacks, and returns which skills it added.
func (l Library) AddFetched(files []File) (added []string, err error) {
	current, err := l.Snapshot()
	if err != nil {
		return nil, err
	}
	have := FetchedSkills(current)
	for _, skill := range FetchedSkills(files) {
		if slices.Contains(have, skill) {
			continue
		}
		for _, f := range files {
			if _, of := split(f.Path); of != skill || !strings.HasPrefix(f.Path, Fetched+"/") {
				continue
			}
			if !safePath(f.Path) {
				return added, fmt.Errorf("%q can't be in a library", f.Path)
			}
			if err := atomicfile.Write(l.Path(f.Path), f.Data, 0o644); err != nil {
				return added, err
			}
		}
		added = append(added, skill)
	}
	return added, nil
}

// Take makes this library hold files from another machine: what you made is replaced as a whole, and each
// fetched skill that came along replaces this machine's copy of it.
func (l Library) Take(files []File) error {
	incoming := map[string]bool{}
	fetchedSkills := map[string]bool{}
	for _, f := range files {
		if !safePath(f.Path) || !shared(f.Path) {
			return fmt.Errorf("%q can't be in a library", f.Path)
		}
		incoming[f.Path] = true
		if top, skill := split(f.Path); top == Fetched {
			fetchedSkills[skill] = true
		}
	}
	current, err := l.Snapshot()
	if err != nil {
		return err
	}
	for _, f := range current {
		_, skill := split(f.Path)
		if incoming[f.Path] || !(yours(f.Path) || fetchedSkills[skill]) {
			continue
		}
		if err := os.Remove(l.Path(f.Path)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	for _, f := range files {
		if err := atomicfile.Write(l.Path(f.Path), f.Data, 0o644); err != nil {
			return err
		}
	}
	for _, dir := range []string{SkillsDir, Fetched} {
		if err := removeEmptyDirs(l.Path(dir)); err != nil {
			return err
		}
	}
	return nil
}

// yours: what you made dates the library; fetched copies don't.
func yours(p string) bool {
	top, _ := split(p)
	return slices.Contains(yourParts, top)
}

func shared(p string) bool {
	top, _ := split(p)
	return yours(p) || top == Fetched
}

// split returns a path's first element and, under a folder, the second: the skill it belongs to.
func split(p string) (top, skill string) {
	parts := strings.SplitN(p, "/", 3)
	if len(parts) > 1 {
		return parts[0], parts[1]
	}
	return parts[0], ""
}

// safePath rejects anything that could land outside the library, or be written differently, on any OS.
func safePath(p string) bool {
	if !fs.ValidPath(p) || p == "." || strings.ContainsAny(p, `\:*?"<>|`) {
		return false
	}
	for _, element := range strings.Split(p, "/") {
		if strings.HasPrefix(element, ".") || strings.HasSuffix(element, " ") || reservedOnWindows(element) {
			return false
		}
	}
	return true
}

func reservedOnWindows(element string) bool {
	stem, _, _ := strings.Cut(strings.ToUpper(element), ".")
	switch stem {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	return len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '0' && stem[3] <= '9'
}

func removeEmptyDirs(root string) error {
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if err := removeEmptyDirs(dir); err != nil {
			return err
		}
		if left, err := os.ReadDir(dir); err == nil && len(left) == 0 {
			if err := os.Remove(dir); err != nil {
				return err
			}
		}
	}
	return nil
}
