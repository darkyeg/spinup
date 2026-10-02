package service

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"

	"github.com/darkyeg/spinup/internal/api"
)

type forwardTarget struct {
	url *url.URL
	// toPeer marks the request so the receiving machine never forwards it again.
	toPeer bool
}

type forwardTargetKey struct{}

func (m *Machine) newForwarder(transport http.RoundTripper) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Transport:     transport,
		FlushInterval: -1, // stream answers as they come
		Rewrite: func(pr *httputil.ProxyRequest) {
			t := pr.In.Context().Value(forwardTargetKey{}).(forwardTarget)
			pr.SetURL(t.url)
			pr.Out.Header.Del(api.ForwardedHeader)
			if t.toPeer {
				pr.Out.Header.Set(api.ForwardedHeader, m.selfName())
			}
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			m.log.Printf("forward %s: %v", r.URL.Path, err)
			writeError(w, http.StatusBadGateway, "the machine holding the accounts didn't answer: "+err.Error())
		},
	}
}

// forward sends everything that isn't spinup's own (the API, the dashboard) to whoever holds the accounts.
func (m *Machine) forward(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	leading, leaderAddr := m.leading, m.leaderAddr
	m.mu.Unlock()
	var t forwardTarget
	switch {
	case leading:
		t.url = &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(m.cfg.ProxyPort))}
	case r.Header.Get(api.ForwardedHeader) != "":
		writeError(w, http.StatusServiceUnavailable, "this machine doesn't hold the accounts any more; retry")
		return
	case leaderAddr == "":
		writeError(w, http.StatusServiceUnavailable, "no machine holds the accounts right now (`spinup status` says why)")
		return
	default:
		t = forwardTarget{url: &url.URL{Scheme: "http", Host: leaderAddr}, toPeer: true}
	}
	m.forwarder.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), forwardTargetKey{}, t)))
}
