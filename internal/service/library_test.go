package service

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
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

func TestDeletingLastLibraryFileReachesAnotherMachine(t *testing.T) {
	makeShare := func() *libraryShare {
		m := New(Options{Library: library.At(t.TempDir()), StatePath: filepath.Join(t.TempDir(), "state.json"), Log: log.New(io.Discard, "", 0)})
		m.openLibrary()
		return m.library
	}
	source, other := makeShare(), makeShare()
	now := time.Now().UTC()
	path := source.lib.Path(library.Instructions)
	if err := os.WriteFile(path, []byte("instructions"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := source.snapshot(now)
	if err != nil {
		t.Fatal(err)
	}
	if err := other.take(before, now); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	deleted, err := source.snapshot(now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := other.take(deleted, now.Add(time.Minute)); err != nil {
		t.Fatalf("deletion was refused: %v", err)
	}
	if _, err := os.Stat(other.lib.Path(library.Instructions)); !os.IsNotExist(err) {
		t.Fatalf("the deleted instructions remain: %v", err)
	}
	otherState, err := other.state(now.Add(2 * time.Minute))
	if err != nil || otherState.Stamp != deleted.Stamp {
		t.Fatalf("received deletion = %+v, %v; want %+v", otherState.Stamp, err, deleted.Stamp)
	}
}

func TestAPartnerThatLosesFetchedCopiesGetsThemOnce(t *testing.T) {
	m := New(Options{Library: library.At(t.TempDir()), StatePath: filepath.Join(t.TempDir(), "state.json"), Log: log.New(io.Discard, "", 0)})
	m.openLibrary()
	putFile(t, m.library.lib.Path(library.Fetched+"/caveman/SKILL.md"), "fetched copy")
	here, err := m.library.state(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var given atomic.Int32
	partner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == api.PathLibraryState:
			writeJSON(w, api.LibraryState{Stamp: here.Stamp})
		case r.Method == http.MethodPost && r.URL.Path == api.PathLibrary:
			given.Add(1)
			writeJSON(w, okBody)
		default:
			http.NotFound(w, r)
		}
	}))
	defer partner.Close()
	for range 5 {
		m.compareLibrary(t.Context(), partner.URL)
	}
	if n := given.Load(); n != 1 {
		t.Fatalf("the library was sent %d times to a partner that can't keep it; want once", n)
	}
}

func TestTheWholeLibraryGoesToAMachineAtMostOncePerGap(t *testing.T) {
	m := New(Options{Library: library.At(t.TempDir()), StatePath: filepath.Join(t.TempDir(), "state.json"), Log: log.New(io.Discard, "", 0), LibraryEvery: time.Hour})
	m.openLibrary()
	fetch := func(from string) int {
		r := httptest.NewRequest(http.MethodGet, api.PathLibrary, nil)
		r.Header.Set(api.ForwardedHeader, from)
		w := httptest.NewRecorder()
		m.serveLibrary(w, r)
		return w.Code
	}
	if code := fetch("lenovo"); code != http.StatusOK {
		t.Fatalf("first fetch = %d", code)
	}
	if code := fetch("lenovo"); code != http.StatusTooManyRequests {
		t.Fatalf("a second fetch right away = %d; want %d", code, http.StatusTooManyRequests)
	}
	if code := fetch("laptop"); code != http.StatusOK {
		t.Fatalf("another machine's fetch = %d", code)
	}
}

func putFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
