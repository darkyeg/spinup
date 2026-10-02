// Package logins reads and merges CLIProxyAPI account logins, one JSON file per account.
//
// A copy replaces another only when it was refreshed later, so syncing can never undo a refresh.
package logins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darkyeg/spinup/internal/atomicfile"
)

type File struct {
	Name string          `json:"name"`
	Data json.RawMessage `json:"data"`
}

// validName accepts only a plain "<name>.json" inside the folder: names arrive over the network.
func validName(name string) bool {
	return name != "" && filepath.Base(name) == name && !strings.ContainsAny(name, `/\:`) &&
		strings.HasSuffix(name, ".json") && !strings.HasPrefix(name, ".")
}

// Read returns every login in dir; a file that is mid-write or not JSON waits for the next read.
func Read(dir string) ([]File, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []File
	for _, e := range entries {
		if e.IsDir() || !validName(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil || !json.Valid(data) {
			continue
		}
		files = append(files, File{Name: e.Name(), Data: data})
	}
	return files, nil
}

type Merged struct {
	Written []string
	Removed []string
}

func (m Merged) Changed() bool { return len(m.Written)+len(m.Removed) > 0 }

// Extent says whether incoming logins are everything the sender has.
type Extent int

const (
	Some Extent = iota
	Everything
)

// Merge writes each incoming login that is new here or refreshed later than ours. When incoming is
// Everything, logins the sender no longer has move to removedDir.
func Merge(dir, removedDir string, incoming []File, extent Extent) (Merged, error) {
	var m Merged
	for _, f := range incoming {
		if !validName(f.Name) || !json.Valid(f.Data) {
			return m, fmt.Errorf("refusing login %q: bad name or not JSON", f.Name)
		}
	}
	local, err := Read(dir)
	if err != nil {
		return m, err
	}
	have := map[string][]byte{}
	for _, f := range local {
		have[f.Name] = f.Data
	}
	sent := map[string]bool{}
	for _, f := range incoming {
		sent[f.Name] = true
		path := filepath.Join(dir, f.Name)
		if cur, ok := have[f.Name]; ok && !replaces(f.Data, cur) {
			continue
		}
		// A running proxy may have refreshed the file since it was read: decide again on what's there now.
		if now, err := os.ReadFile(path); err == nil && !replaces(f.Data, now) {
			continue
		}
		if err := atomicfile.Write(path, f.Data, 0o600); err != nil {
			return m, err
		}
		m.Written = append(m.Written, f.Name)
	}
	// An empty complete set is more likely a broken sender than a wish to log out of everything.
	if extent == Everything && len(incoming) > 0 {
		for name := range have {
			if sent[name] {
				continue
			}
			if err := os.MkdirAll(removedDir, 0o700); err != nil {
				return m, err
			}
			if err := os.Rename(filepath.Join(dir, name), filepath.Join(removedDir, name)); err != nil {
				return m, err
			}
			m.Removed = append(m.Removed, name)
		}
	}
	sort.Strings(m.Written)
	sort.Strings(m.Removed)
	return m, nil
}

// replaces reports whether incoming was refreshed after local. Undatable copies never replace.
func replaces(incoming, local []byte) bool {
	if string(incoming) == string(local) {
		return false
	}
	in, cur := refreshedAt(incoming), refreshedAt(local)
	return !in.IsZero() && !cur.IsZero() && in.After(cur)
}

// Fingerprint changes whenever any login in dir changes.
func Fingerprint(dir string) string {
	files, _ := Read(dir)
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.Name))
		h.Write([]byte{0})
		h.Write(f.Data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
