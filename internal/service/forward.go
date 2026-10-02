package service

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"time"

	"github.com/darkyeg/spinup/internal/api"
)

// replayBodyUpTo is the largest request body kept in memory so the request can be sent again.
const replayBodyUpTo = 32 << 20

func (m *Machine) newForwarder(transport http.RoundTripper) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Transport: holdingTransport{
			base: transport, route: m.destination, self: m.tail.selfName, within: m.holdLimit, every: holdEvery,
		},
		FlushInterval: -1, // stream answers as they come
		Rewrite:       func(pr *httputil.ProxyRequest) { pr.Out.Host = "" },
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			switch {
			case errors.Is(err, errNotHolding):
				w.Header().Set(api.NotHoldingHeader, "1")
				writeError(w, http.StatusServiceUnavailable, err.Error())
			case errors.Is(err, errNoLeader):
				writeError(w, http.StatusServiceUnavailable, err.Error())
			default:
				m.log.Printf("forward %s: %v", r.URL.Path, err)
				writeError(w, http.StatusBadGateway, "the machine holding the accounts didn't answer: "+err.Error())
			}
		},
	}
}

// destination is the local proxy while this machine leads and takes new requests, otherwise the leader it knows.
func (m *Machine) destination() (destination, bool) {
	if m.ledger.view(time.Now()).Leading {
		end, ok := m.activity.begin()
		if !ok {
			return destination{}, false
		}
		proxyAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(m.cfg.ProxyPort))
		return destination{url: &url.URL{Host: proxyAddr}, end: end}, true
	}
	if _, leaderAddr := m.notes.get(); leaderAddr != "" {
		return destination{url: &url.URL{Host: leaderAddr}, toPeer: true}, true
	}
	return destination{}, false
}

// holdLimit: while another machine may take the accounts over, a request waits out the takeover.
func (m *Machine) holdLimit() time.Duration {
	if m.takeoverPossible() {
		return m.cfg.FailoverAfter() + holdWithin
	}
	return holdWithin
}

// takeoverPossible once there was a leader: a holder can take over itself, and a machine that only uses
// the accounts can't see the standbys, so it presumes one.
func (m *Machine) takeoverPossible() bool {
	if m.cfg.Hold.CanHold() {
		return m.ledger.view(time.Now()).Leader != ""
	}
	return m.notes.sawLeader()
}

// forward sends everything that isn't spinup's own (the API, the dashboard) to whoever holds the accounts.
func (m *Machine) forward(w http.ResponseWriter, r *http.Request) {
	if err := keepBody(r); err != nil {
		writeError(w, http.StatusBadRequest, "couldn't read the request: "+err.Error())
		return
	}
	m.forwarder.ServeHTTP(w, r)
}

// keepBody makes a request that fits in replayBodyUpTo sendable again: a larger body is sent once.
func keepBody(r *http.Request) error {
	if r.Body == nil || r.Body == http.NoBody || r.ContentLength > replayBodyUpTo {
		return nil
	}
	kept, err := io.ReadAll(io.LimitReader(r.Body, replayBodyUpTo+1))
	if err != nil {
		return err
	}
	if len(kept) > replayBodyUpTo {
		r.Body = readThenClose{io.MultiReader(bytes.NewReader(kept), r.Body), r.Body}
		return nil
	}
	r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(kept)), nil }
	r.Body, _ = r.GetBody()
	r.ContentLength = int64(len(kept))
	return nil
}

type readThenClose struct {
	io.Reader
	io.Closer
}
