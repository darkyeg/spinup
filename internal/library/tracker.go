package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
	"time"

	"github.com/darkyeg/spinup/internal/atomicfile"
)

// Tracker remembers when the library last changed, across restarts: a change it sees is dated when it
// sees it, and a library taken from another machine keeps that machine's date.
type Tracker struct {
	path string
	mu   sync.Mutex
	mem  memory
	// remembers is false until the tracker has a memory: then a library is dated by its files, not by now,
	// so an old library on a machine that lost its memory never passes for a new one.
	remembers bool
}

type memory struct {
	Last Stamp `json:"last"`
	// Taking: a library from another machine was being written. Until that finishes, this one reads as
	// untouched, so a half-written library never passes for a change and is taken again.
	Taking bool `json:"taking,omitempty"`
}

// NewTracker keeps its memory in the file at path. A memory it can't read is forgotten, with the error:
// the library is then dated by its files, as on a first run.
func NewTracker(path string) (*Tracker, error) {
	t := &Tracker{path: path}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return t, nil
	case err != nil:
		return t, err
	}
	if err := json.Unmarshal(data, &t.mem); err != nil {
		t.mem = memory{}
		return t, fmt.Errorf("%s: %w", path, err)
	}
	t.remembers = true
	return t, nil
}

// Observe dates the library holding hash, last modified at modified: an empty library is never dated (nor
// remembered), and a new hash is dated now, or by modified when the tracker has no memory yet.
func (t *Tracker) Observe(hash string, modified, now time.Time) (Stamp, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch {
	case t.mem.Taking, hash == "":
		return Stamp{}, nil
	case hash != t.mem.Last.Hash && !t.remembers:
		return t.date(hash, earlier(modified, now))
	case hash != t.mem.Last.Hash:
		return t.date(hash, now)
	}
	return t.mem.Last, nil
}

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// date needs t.mu held.
func (t *Tracker) date(hash string, at time.Time) (Stamp, error) {
	fresh := Stamp{ChangedAt: at.UTC(), Hash: hash}
	return fresh, t.remember(memory{Last: fresh})
}

// Taking records that another machine's library is about to be written; Took, that it was.
func (t *Tracker) Taking(s Stamp) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.remember(memory{Last: s, Taking: true})
}

func (t *Tracker) Took(s Stamp) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.remember(memory{Last: s})
}

// remember needs t.mu held.
func (t *Tracker) remember(m memory) error {
	if m == t.mem && t.remembers {
		return nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := atomicfile.Write(t.path, data, 0o600); err != nil {
		return err
	}
	t.mem, t.remembers = m, true
	return nil
}
