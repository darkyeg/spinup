package service

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/logins"
)

// audience is who can reach a listener: this machine only, or the whole tailnet.
type audience int

const (
	onLocalhost audience = iota
	onTailnet
)

const (
	loginsLimit  = 64 << 20
	commandLimit = 4 << 10
	// handoffTimeout covers the leader waiting for its proxy to go quiet and the target starting its own.
	handoffTimeout = 2 * time.Minute
)

func (m *Machine) routes(a audience) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+api.PathLeader, m.serveLeader)
	mux.HandleFunc("GET "+api.PathLogins, m.keyed(m.holding(m.serveLogins)))
	mux.HandleFunc("POST "+api.PathLogins, m.keyed(m.holding(m.acceptLogins)))
	mux.HandleFunc("GET "+api.PathSecrets, m.keyed(m.serveSecrets))
	mux.HandleFunc("POST "+api.PathReceive, m.keyed(m.holding(m.acceptAccounts)))
	mux.HandleFunc("POST "+api.PathHandoff, m.keyed(m.serveHandoff))
	switch a {
	case onLocalhost:
		mux.HandleFunc("GET "+api.PathState, m.serveState)
		// Only the user at this machine may force it to lead or stop it.
		mux.HandleFunc("POST "+api.PathTakeover, m.keyed(m.holding(m.serveTakeover)))
		mux.HandleFunc("POST "+api.PathStop, m.keyed(m.holding(m.serveStop)))
		mux.HandleFunc("POST "+api.PathRestart, m.keyed(m.holding(m.serveRestart)))
	case onTailnet:
		mux.HandleFunc("GET "+api.PathState, m.keyed(m.serveState))
	}
	mux.HandleFunc("/spinup/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "no such spinup call here")
	})
	mux.HandleFunc("/", m.forward)
	return mux
}

// keyed admits callers with the management password, and remembers callers that can hold.
func (m *Machine) keyed(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		want := m.o.Secrets.ManagementPassword
		got := r.Header.Get(api.KeyHeader)
		if want == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			writeError(w, http.StatusUnauthorized, "missing or wrong "+api.KeyHeader)
			return
		}
		m.learn(r.Header.Get(api.ForwardedHeader), config.Hold(r.Header.Get(api.HoldHeader)))
		h(w, r)
	}
}

// holding refuses requests that only a machine able to hold the accounts can serve.
func (m *Machine) holding(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !m.cfg.Hold.CanHold() {
			writeError(w, http.StatusConflict, "this machine never holds the accounts (install it with --standby)")
			return
		}
		h(w, r)
	}
}

func (m *Machine) serveLeader(w http.ResponseWriter, r *http.Request) {
	st := m.ledger.view(time.Now())
	l := api.Leader{
		Name: m.tail.selfName(), Hold: m.cfg.Hold, Leading: st.Leading, Starting: st.Starting,
		Leader: st.Leader, Epoch: st.Epoch,
	}
	writeJSON(w, l.Prove(m.o.Secrets, r.URL.Query().Get(api.NonceParam)))
}

func (m *Machine) serveState(w http.ResponseWriter, _ *http.Request) { writeJSON(w, m.Report()) }

func (m *Machine) serveLogins(w http.ResponseWriter, _ *http.Request) {
	own, err := m.ownLogins(logins.Everything)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, own)
}

func (m *Machine) acceptLogins(w http.ResponseWriter, r *http.Request) {
	var incoming api.Logins
	if !decode(w, r, loginsLimit, &incoming) {
		return
	}
	if m.fromLeader(incoming) {
		m.mergeFromLeader(incoming)
	} else if _, err := logins.Merge(m.cfg.AuthDir, m.cfg.RemovedDir(), incoming.Files, logins.Some); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, okBody)
}

func (m *Machine) serveSecrets(w http.ResponseWriter, _ *http.Request) { writeJSON(w, m.o.Secrets) }

func (m *Machine) acceptAccounts(w http.ResponseWriter, r *http.Request) {
	var accounts api.Logins
	if !decode(w, r, loginsLimit, &accounts) {
		return
	}
	if err := m.receive(accounts); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, okBody)
}

// serveHandoff hands off if this machine leads, and otherwise passes the request to the leader once.
func (m *Machine) serveHandoff(w http.ResponseWriter, r *http.Request) {
	var req api.Handoff
	if !decode(w, r, commandLimit, &req) {
		return
	}
	st := m.ledger.view(time.Now())
	ctx, cancel := context.WithTimeout(r.Context(), handoffTimeout)
	defer cancel()
	var err error
	switch {
	case st.Leading:
		err = m.handOff(ctx, req.To)
	case st.Leader != "" && r.Header.Get(api.ForwardedHeader) == "":
		err = m.callPeer(ctx, st.Leader, api.PathHandoff, req, nil)
	default:
		err = errors.New("no machine holds the accounts right now")
	}
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, okBody)
}

func (m *Machine) serveTakeover(w http.ResponseWriter, _ *http.Request) {
	if err := m.ledger.takeover(time.Now()); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	m.log.Printf("the user asked this machine to take the accounts")
	writeJSON(w, okBody)
}

func (m *Machine) serveStop(w http.ResponseWriter, _ *http.Request) {
	m.log.Printf("the user asked the service to stop")
	writeJSON(w, okBody)
	_ = http.NewResponseController(w).Flush()
	m.stopRun()
}

func (m *Machine) serveRestart(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), handoffTimeout)
	defer cancel()
	if err := m.restartProxy(ctx); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, okBody)
}

var okBody = map[string]bool{"ok": true}

func decode(w http.ResponseWriter, r *http.Request, limit int64, v any) bool {
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit)).Decode(v)
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "request too large")
	case err != nil:
		writeError(w, http.StatusBadRequest, "bad request: "+err.Error())
	}
	return err == nil
}

func writeJSON(w http.ResponseWriter, v any) { writeJSONStatus(w, http.StatusOK, v) }

func writeError(w http.ResponseWriter, status int, message string) {
	var e api.Error
	e.Error.Message = message
	writeJSONStatus(w, status, e)
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
