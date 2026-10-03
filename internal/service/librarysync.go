package service

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/library"
)

const (
	libraryCallWithin = time.Minute
	// partnersWithin is how long catching up waits for a machine to share with, right after the service started.
	partnersWithin = 10 * time.Second
)

// shareLibrary compares libraries with the partners every while and installs what arrives, one install at a
// time, until ctx ends. An install that fails is tried again each round.
func (m *Machine) shareLibrary(ctx context.Context) {
	ticker := time.NewTicker(m.o.LibraryEvery)
	defer ticker.Stop()
	failing := ""
	for {
		m.compareLibraries(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if failing != "" {
				failing = m.installLibrary(ctx, failing)
			}
		case <-m.library.installWanted:
			failing = m.installLibrary(ctx, failing)
		}
	}
}

// installLibrary installs the library and returns why it failed, logging a reason once.
func (m *Machine) installLibrary(ctx context.Context, failedBefore string) (failing string) {
	err := m.o.ApplyLibrary(ctx)
	switch {
	case err == nil && failedBefore != "":
		m.log.Printf("library: installed")
	case err != nil && err.Error() != failedBefore:
		m.log.Printf("library: installing it: %v (trying again)", err)
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

// catchUpLibrary compares with the partners now, waiting a little for one to show up right after a start.
func (m *Machine) catchUpLibrary(ctx context.Context) {
	for deadline := m.library.opened.Add(partnersWithin); len(m.libraryPartners()) == 0 && time.Now().Before(deadline); {
		if !sleep(ctx, m.o.Tick/4) {
			return
		}
	}
	m.compareLibraries(ctx)
}

// libraryPartners: a holder shares with the holders that proved themselves, any other machine with the leader.
func (m *Machine) libraryPartners() []string {
	if !m.cfg.Hold.CanHold() {
		if _, addr := m.notes.get(); addr != "" {
			return []string{"http://" + addr}
		}
		return nil
	}
	var bases []string
	for _, name := range m.peers.holders() {
		if base, err := m.provenURL(name, ""); err == nil {
			bases = append(bases, base)
		}
	}
	return bases
}

func (m *Machine) compareLibraries(ctx context.Context) {
	for _, base := range m.libraryPartners() {
		m.compareLibrary(ctx, base)
	}
}

// compareLibrary takes the partner's library when it is newer and gives this one when it is; with the same
// library, each side passes on the fetched copies the other lacks.
func (m *Machine) compareLibrary(ctx context.Context, base string) {
	ctx, cancel := context.WithTimeout(ctx, libraryCallWithin)
	defer cancel()
	here, err := m.library.state(time.Now())
	if err != nil {
		m.log.Printf("library: %v", err)
		return
	}
	var there api.LibraryState
	if err := m.libraryClient().Get(ctx, base+api.PathLibraryState, &there); err != nil {
		return
	}
	switch library.Compare(here.Stamp, there.Stamp) {
	case library.Take:
		if incoming, ok := m.fetchLibrary(ctx, base); ok {
			m.logTake(m.library.take(incoming, time.Now()), incoming.Stamp)
		}
	case library.Give:
		m.giveLibrary(ctx, base)
	default:
		if lacking(here.Fetched, there.Fetched) {
			if incoming, ok := m.fetchLibrary(ctx, base); ok {
				m.logFetchedCopies(m.library.addFetched(incoming.Files))
			}
		}
		if lacking(there.Fetched, here.Fetched) {
			m.giveLibrary(ctx, base)
		}
	}
}

// lacking reports whether have misses any of offered.
func lacking(have, offered []string) bool {
	return slices.ContainsFunc(offered, func(name string) bool { return !slices.Contains(have, name) })
}

func (m *Machine) fetchLibrary(ctx context.Context, base string) (api.Library, bool) {
	var incoming api.Library
	if err := m.libraryClient().Get(ctx, base+api.PathLibrary, &incoming); err != nil {
		m.log.Printf("library: couldn't fetch it from %s: %v", base, err)
		return incoming, false
	}
	return incoming, true
}

func (m *Machine) giveLibrary(ctx context.Context, base string) {
	mine, err := m.library.snapshot(time.Now())
	if err != nil {
		m.log.Printf("library: %v", err)
		return
	}
	if err := m.libraryClient().Post(ctx, base+api.PathLibrary, mine, nil); err != nil && !api.WasRefused(err) {
		m.log.Printf("library: couldn't share it with %s: %v", base, err)
	}
}

func (m *Machine) logTake(err error, stamp library.Stamp) {
	switch {
	case err == nil:
		m.log.Printf("library: took the one changed %s", stamp.ChangedAt.Local().Format(time.DateTime))
	case !errors.Is(err, errNotNewer):
		m.log.Printf("library: couldn't take it: %v", err)
	}
}

func (m *Machine) logFetchedCopies(added []string, err error) {
	if len(added) > 0 {
		m.log.Printf("library: got copies of %v", added)
	}
	if err != nil {
		m.log.Printf("library: storing copies: %v", err)
	}
}

func (m *Machine) libraryClient() api.Client {
	return api.Client{HTTP: m.httpClient, APIKey: m.o.Secrets.APIKey, From: m.tail.selfName()}
}
