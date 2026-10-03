package service

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/library"
)

func TestTakenLibraryKeepsItsAgeAfterTrackerLoss(t *testing.T) {
	for _, loss := range []string{"deleted", "corrupt"} {
		t.Run(loss, func(t *testing.T) {
			dir := t.TempDir()
			m := New(Options{Library: library.At(t.TempDir()), StatePath: filepath.Join(dir, "state.json"), Log: log.New(io.Discard, "", 0)})
			m.openLibrary()
			now := time.Now().UTC().Truncate(time.Second)
			files := []library.File{{Path: library.Instructions, Data: []byte("earlier instructions")}}
			taken := api.Library{Stamp: library.Stamp{ChangedAt: now.Add(-time.Hour), Hash: library.Fingerprint(files)}, Files: files}
			if err := m.library.take(taken, now); err != nil {
				t.Fatal(err)
			}
			memory := filepath.Join(dir, "library.json")
			if loss == "deleted" {
				if err := os.Remove(memory); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(memory, []byte("not json"), 0o600); err != nil {
				t.Fatal(err)
			}
			m.openLibrary()
			recovered, err := m.library.snapshot(now.Add(time.Minute))
			if err != nil || recovered.Stamp != taken.Stamp {
				t.Fatalf("a received library became newer after tracker loss: %+v, %v; want %+v", recovered.Stamp, err, taken.Stamp)
			}
			files = []library.File{{Path: library.Instructions, Data: []byte("newer instructions")}}
			newer := api.Library{Stamp: library.Stamp{ChangedAt: now.Add(-30 * time.Minute), Hash: library.Fingerprint(files)}, Files: files}
			if err := m.library.take(newer, now.Add(time.Minute)); err != nil {
				t.Fatalf("the actual newer library was refused: %v", err)
			}
		})
	}
}
