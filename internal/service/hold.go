package service

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/darkyeg/spinup/internal/api"
)

const (
	// holdWithin is how long a request waits for a leader during a hand-off; a takeover adds the failover time.
	holdWithin = time.Minute
	holdEvery  = 250 * time.Millisecond
)

var (
	errNotHolding = errors.New("this machine doesn't hold the accounts any more; retry")
	errNoLeader   = errors.New("no machine holds the accounts right now (`spinup status` says why)")
)

// destination is where a request goes right now.
type destination struct {
	url    *url.URL
	toPeer bool
	// end uncounts a request served by the local proxy; nil toward a peer.
	end func()
}

// holdingTransport sends each request to whoever holds the accounts. While they move it waits for
// the new leader, and retries only what cannot have been served: a request nobody could take, a
// refused connection, or a peer that said it no longer holds the accounts. It decides before any
// response reaches the client, so the client never sees the switch.
type holdingTransport struct {
	base  http.RoundTripper
	route func() (destination, bool)
	self  func() string
	// within bounds the wait; every is the pause between looks for a leader.
	within func() time.Duration
	every  time.Duration
}

func (t holdingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	deadline := time.Now().Add(t.within())
	fromPeer := req.Header.Get(api.ForwardedHeader) != ""
	for {
		resp, err := t.attempt(req, fromPeer)
		if !transient(resp, err) || (tried(err) && !replayable(req)) {
			return resp, err
		}
		if req.Context().Err() != nil || !time.Now().Before(deadline) {
			return resp, err
		}
		if resp != nil {
			resp.Body.Close()
		}
		if !sleep(req.Context(), t.every) {
			return nil, req.Context().Err()
		}
	}
}

// attempt: a request that came from a peer is never passed on to another peer.
func (t holdingTransport) attempt(req *http.Request, fromPeer bool) (*http.Response, error) {
	dest, ok := t.route()
	switch {
	case fromPeer && (!ok || dest.toPeer):
		return nil, errNotHolding
	case !ok:
		return nil, errNoLeader
	}
	return t.send(req, dest)
}

func (t holdingTransport) send(req *http.Request, dest destination) (*http.Response, error) {
	out := req.Clone(req.Context())
	out.URL.Scheme, out.URL.Host = "http", dest.url.Host
	out.Header.Del(api.ForwardedHeader)
	if dest.toPeer {
		out.Header.Set(api.ForwardedHeader, t.self())
	}
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		out.Body = body
	}
	resp, err := t.base.RoundTrip(out)
	switch {
	case err != nil:
		uncount(dest)
		return nil, err
	case resp.StatusCode == http.StatusSwitchingProtocols:
		uncount(dest)
	case dest.end != nil:
		resp.Body = endsWith{resp.Body, dest.end}
	}
	return resp, nil
}

func uncount(d destination) {
	if d.end != nil {
		d.end()
	}
}

// tried reports whether the attempt reached the point of sending the request.
func tried(err error) bool { return !errors.Is(err, errNoLeader) }

func transient(resp *http.Response, err error) bool {
	if resp != nil {
		return resp.StatusCode == http.StatusServiceUnavailable && resp.Header.Get(api.NotHoldingHeader) != ""
	}
	var op *net.OpError
	return errors.Is(err, errNoLeader) || (errors.As(err, &op) && op.Op == "dial")
}

// endsWith uncounts its request when the answer has been read or dropped.
type endsWith struct {
	io.ReadCloser
	end func()
}

func (e endsWith) Close() error {
	defer e.end()
	return e.ReadCloser.Close()
}

func replayable(req *http.Request) bool {
	return req.GetBody != nil || req.Body == nil || req.Body == http.NoBody
}
