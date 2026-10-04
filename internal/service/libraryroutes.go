package service

import (
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/library"
)

// libraryLimit bounds a library on the wire: its files, base64-encoded.
const libraryLimit = library.SizeLimit * 3 / 2

// withAPIKey admits callers that know the API key: every machine of yours.
func (m *Machine) withAPIKey(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		want := m.o.Secrets.APIKey
		got := r.Header.Get(api.APIKeyHeader)
		if want == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			writeError(w, http.StatusUnauthorized, "missing or wrong "+api.APIKeyHeader)
			return
		}
		h(w, r)
	}
}

func (m *Machine) serveLibraryState(w http.ResponseWriter, _ *http.Request) {
	state, err := m.library.state(time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, state)
}

// serveLibrary sends the whole library to a machine at most once per gap, so a partner stuck re-fetching
// it can't flood the link; a partner fetches once a round, so a third of one still lets a catch-up through.
func (m *Machine) serveLibrary(w http.ResponseWriter, r *http.Request) {
	if !m.library.exchanges.mayServe(caller(r), time.Now(), m.o.LibraryEvery/3) {
		writeError(w, http.StatusTooManyRequests, "this machine got the library moments ago; it gets it again next round")
		return
	}
	l, err := m.library.snapshot(time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, l)
}

// caller is the machine that sent r: the name it gives, else its address.
func caller(r *http.Request) string {
	if name := r.Header.Get(api.ForwardedHeader); name != "" {
		return name
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

// acceptLibrary takes a newer library, or else keeps the fetched copies it brings that this machine lacks.
func (m *Machine) acceptLibrary(w http.ResponseWriter, r *http.Request) {
	var incoming api.Library
	if !decode(w, r, libraryLimit, &incoming) {
		return
	}
	err := m.library.take(incoming, time.Now())
	m.logTake(err, incoming.Stamp)
	if errors.Is(err, errNotNewer) {
		added, addErr := m.library.addFetched(incoming.Files)
		m.logFetchedCopies(added, addErr)
		err = addErr
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "library: "+err.Error())
		return
	}
	writeJSON(w, okBody)
}

// serveLibrarySync catches up with the other machines before the user edits the library here.
func (m *Machine) serveLibrarySync(w http.ResponseWriter, r *http.Request) {
	m.catchUpLibrary(r.Context())
	writeJSON(w, okBody)
}
