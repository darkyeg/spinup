package service

import (
	"errors"
	"path/filepath"
	"sync"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/library"
)

var errNotNewer = errors.New("this machine's library is as new or newer")

// libraryShare is this machine's library as the service shares it: it reads and dates the library, and
// stores what arrives from other machines, one look or change at a time.
type libraryShare struct {
	lib     library.Library
	tracker *library.Tracker
	opened  time.Time
	mu      sync.Mutex
	// installWanted asks the sharing loop to install the library; it holds at most one request.
	installWanted chan struct{}
}

// openLibrary starts sharing when there is a library to share; a memory it can't read is only logged.
func (m *Machine) openLibrary() {
	if m.o.Library.Dir() == "" {
		return
	}
	tracker, err := library.NewTracker(filepath.Join(filepath.Dir(m.o.StatePath), "library.json"))
	if err != nil {
		m.log.Printf("library: starting without memory: %v", err)
	}
	m.library = &libraryShare{lib: m.o.Library, tracker: tracker, opened: time.Now(), installWanted: make(chan struct{}, 1)}
}

// current reads the library and dates it; needs s.mu held.
func (s *libraryShare) current(now time.Time) (api.Library, error) {
	files, err := s.lib.Snapshot()
	if err != nil {
		return api.Library{}, err
	}
	stamp, err := s.tracker.Observe(library.Fingerprint(files), library.LastModified(files), now)
	return api.Library{Stamp: stamp, Files: files}, err
}

func (s *libraryShare) state(now time.Time) (api.LibraryState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, err := s.current(now)
	return api.LibraryState{Stamp: l.Stamp, Fetched: library.FetchedSkills(l.Files)}, err
}

func (s *libraryShare) snapshot(now time.Time) (api.Library, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current(now)
}

// take stores incoming when it is newer than this machine's library, and errNotNewer otherwise.
func (s *libraryShare) take(incoming api.Library, now time.Time) error {
	switch {
	case library.Fingerprint(incoming.Files) != incoming.Stamp.Hash:
		return errors.New("the library's files don't match its stamp")
	case incoming.Stamp.FromTheFuture(now):
		return errors.New("the library is dated in the future; check the clocks")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	here, err := s.current(now)
	if err != nil {
		return err
	}
	if library.Compare(here.Stamp, incoming.Stamp) != library.Take {
		return errNotNewer
	}
	if err := s.tracker.Taking(incoming.Stamp); err != nil {
		return err
	}
	if err := s.lib.Take(incoming.Files); err != nil {
		return err
	}
	if err := s.tracker.Took(incoming.Stamp); err != nil {
		return err
	}
	s.wantInstall()
	return nil
}

// addFetched stores the fetched copies in files that this library lacks.
func (s *libraryShare) addFetched(files []library.File) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	added, err := s.lib.AddFetched(files)
	if len(added) > 0 {
		s.wantInstall()
	}
	return added, err
}

func (s *libraryShare) wantInstall() {
	select {
	case s.installWanted <- struct{}{}:
	default:
	}
}
