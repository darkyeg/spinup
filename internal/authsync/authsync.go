// Package authsync copies CLIProxyAPI account logins (one JSON file per account) between machines.
//
// The rule that keeps refresh tokens alive: a copy only replaces another when it is newer, judged by
// the time the login was last refreshed. An older copy never overwrites a newer one, in either
// direction, so syncing can't undo a refresh.
package authsync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/darkyeg/spinup/internal/config"
)

// File is one account login.
type File struct {
	Name string          `json:"name"`
	Data json.RawMessage `json:"data"`
}

// Bundle is a set of logins sent between machines.
type Bundle struct {
	From  string `json:"from"`
	Epoch int64  `json:"epoch"`
	// Full means Files is everything the sender has, so files missing from it were removed there.
	Full  bool   `json:"full"`
	Files []File `json:"files"`
}

// ValidName rejects anything that isn't a plain "<name>.json" in the folder itself: names arrive
// over the network and must never point elsewhere on disk.
func ValidName(name string) bool {
	return name != "" && filepath.Base(name) == name && !strings.ContainsAny(name, `/\:`) &&
		strings.HasSuffix(name, ".json") && !strings.HasPrefix(name, ".")
}

// Read returns every login in dir (sub-folders such as logs/ are ignored).
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
		if e.IsDir() || !ValidName(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil || !json.Valid(data) {
			continue // being written, or not a login: skip this round
		}
		files = append(files, File{Name: e.Name(), Data: data})
	}
	return files, nil
}

// stampFields are the login fields that say when it was last refreshed, best first.
var stampFields = []string{"last_refresh", "last_refreshed", "updated_at", "expired", "expire", "expires_at", "expiry"}

// Stamp is when the login was last refreshed (zero if unknown).
func Stamp(data []byte) time.Time {
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return time.Time{}
	}
	for _, f := range stampFields {
		if t := parseTime(m[f]); !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

func parseTime(v any) time.Time {
	switch x := v.(type) {
	case string:
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
			if t, err := time.Parse(layout, x); err == nil {
				return t
			}
		}
		if n, err := strconv.ParseInt(x, 10, 64); err == nil {
			return unix(n)
		}
	case float64:
		return unix(int64(x))
	}
	return time.Time{}
}

func unix(n int64) time.Time {
	if n > 1e12 { // milliseconds
		return time.UnixMilli(n)
	}
	if n > 0 {
		return time.Unix(n, 0)
	}
	return time.Time{}
}

// RefreshToken is the login's refresh token, used to tell copies apart.
func RefreshToken(data []byte) string {
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return ""
	}
	s, _ := m["refresh_token"].(string)
	return s
}

// Newer reports whether incoming should replace local.
func Newer(incoming, local []byte) bool {
	if string(incoming) == string(local) {
		return false
	}
	in, cur := Stamp(incoming), Stamp(local)
	if in.IsZero() || cur.IsZero() {
		return false // can't tell which is newer: keep what we have
	}
	return in.After(cur)
}

// Result says what Apply changed.
type Result struct {
	Written []string
	Removed []string
}

// Apply merges incoming logins into dir: a file is written when it is new here or newer than ours.
// With full, logins we have that the sender doesn't are moved to removedDir (never deleted).
func Apply(dir, removedDir string, incoming []File, full bool) (Result, error) {
	var res Result
	local, err := Read(dir)
	if err != nil {
		return res, err
	}
	have := map[string][]byte{}
	for _, f := range local {
		have[f.Name] = f.Data
	}
	seen := map[string]bool{}
	for _, f := range incoming {
		if !ValidName(f.Name) || !json.Valid(f.Data) {
			return res, fmt.Errorf("refusing login %q: bad name or not JSON", f.Name)
		}
		seen[f.Name] = true
		cur, ok := have[f.Name]
		if ok && !Newer(f.Data, cur) {
			continue
		}
		if err := config.WriteFileAtomic(filepath.Join(dir, f.Name), f.Data, 0o600); err != nil {
			return res, err
		}
		res.Written = append(res.Written, f.Name)
	}
	if full && len(incoming) > 0 { // an empty "full" bundle is more likely a broken sender than a wipe
		for name := range have {
			if seen[name] {
				continue
			}
			if err := os.MkdirAll(removedDir, 0o700); err != nil {
				return res, err
			}
			if err := os.Rename(filepath.Join(dir, name), filepath.Join(removedDir, name)); err != nil {
				return res, err
			}
			res.Removed = append(res.Removed, name)
		}
	}
	sort.Strings(res.Written)
	sort.Strings(res.Removed)
	return res, nil
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

// Summary describes a login without its secrets.
type Summary struct {
	Name      string    `json:"name"`
	Type      string    `json:"type,omitempty"`
	Refreshed time.Time `json:"refreshed"`
}

// Summarize lists the logins in dir without tokens.
func Summarize(dir string) []Summary {
	files, _ := Read(dir)
	out := make([]Summary, 0, len(files))
	for _, f := range files {
		var m map[string]any
		_ = json.Unmarshal(f.Data, &m)
		typ, _ := m["type"].(string)
		out = append(out, Summary{Name: f.Name, Type: typ, Refreshed: Stamp(f.Data)})
	}
	return out
}
