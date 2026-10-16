package service

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"sync"
	"time"

	"github.com/darkyeg/spinup/internal/atomicfile"
)

type artifactAttempt struct {
	Count int       `json:"count"`
	First time.Time `json:"first"`
	Next  time.Time `json:"next"`
	Done  bool      `json:"done,omitempty"`
}

// artifactBudget reserves attempts on disk before moving bytes, so restarting a
// failing service cannot restart an unlimited download loop.
type artifactBudget struct {
	mu       sync.Mutex
	path     string
	log      *log.Logger
	entries  map[string]artifactAttempt
	disabled bool
	now      func() time.Time
}

func openArtifactBudget(path string, logger *log.Logger) *artifactBudget {
	b := &artifactBudget{path: path, log: logger, entries: map[string]artifactAttempt{}, now: time.Now}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return b
	}
	if err == nil {
		err = json.Unmarshal(data, &b.entries)
		if err == nil && b.entries == nil {
			err = errors.New("invalid artifact transfer memory")
		}
	}
	if err != nil {
		b.disable(err)
	}
	return b
}

func artifactKey(direction, peer, digest string) string {
	return direction + "\n" + peer + "\n" + digest
}

func (b *artifactBudget) begin(key string, serving bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.disabled {
		return false
	}
	now := b.now()
	a := b.entries[key]
	if serving && !a.First.IsZero() && now.Sub(a.First) >= 24*time.Hour {
		a = artifactAttempt{}
	}
	if a.Done || a.Count >= 3 || now.Before(a.Next) {
		return false
	}
	if a.Count == 0 {
		a.First = now
	}
	a.Count++
	a.Next = now.Add(time.Minute)
	if a.Count > 1 {
		a.Next = now.Add(5 * time.Minute)
	}
	b.entries[key] = a
	return b.save()
}

func (b *artifactBudget) succeeded(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	a := b.entries[key]
	a.Done = true
	b.entries[key] = a
	b.save()
}

// healthy forgets a missing-content episode only after seeing verified content
// at its destination; unrelated library edits must not renew its retry budget.
func (b *artifactBudget) healthy(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.entries[key]; ok {
		delete(b.entries, key)
		b.save()
	}
}

func (b *artifactBudget) save() bool {
	if b.disabled {
		return false
	}
	data, err := json.Marshal(b.entries)
	if err == nil {
		err = atomicfile.Write(b.path, data, 0o600)
	}
	if err != nil {
		b.disable(err)
		return false
	}
	return true
}

func (b *artifactBudget) disable(err error) {
	b.disabled = true
	b.log.Printf("packages: automatic artifact transfers paused: %v; repair transfer memory %s and restart the service, or run spinup packages fetch", err, b.path)
}
