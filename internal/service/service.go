// Package service runs spinup on one machine: the localhost front every machine has and, on
// machines that can hold the accounts, leadership, login sync and the peer API.
package service

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"
	"sync"
	"time"

	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/leadership"
	"github.com/darkyeg/spinup/internal/tailnet"
)

// Proxy is the local CLIProxyAPI; *proxy.Runner is the real one.
type Proxy interface {
	Start(ctx context.Context) error
	// Stop waits for quiet (no login being written) before stopping, within a bound; nil skips the wait.
	Stop(quiet func() bool)
	Running() bool
}

type Options struct {
	Config  config.Config
	Secrets config.Secrets // only the API key is known on machines that never hold
	Tailnet tailnet.Source
	Proxy   Proxy // nil on machines that never hold
	// PrepareProxy writes the proxy's config before each start.
	PrepareProxy func() error
	Log          *log.Logger
	Version      string
	StatePath    string

	// Tests reach each machine on its own loopback ports and tick faster.
	PeerAddr    func(name, ip string) string
	FrontListen string
	PeerListen  string
	Tick        time.Duration
}

// Machine is this machine running spinup.
type Machine struct {
	o          Options
	cfg        config.Config
	log        *log.Logger
	httpClient *http.Client
	forwarder  *httputil.ReverseProxy

	transition sync.Mutex // one leadership change at a time

	mu        sync.Mutex
	self      tailnet.Node
	tailnet   tailnet.Status
	state     persisted
	bootAlive time.Time // state.LastAlive when the service started or woke
	leading   bool
	forced    bool
	waiting   string
	// cutOffSince is when this machine lost Tailscale; zero while connected.
	cutOffSince time.Time
	// leaderAddr is the peer API of whoever holds the accounts; empty when unknown.
	leaderAddr string
	peers      peerView
	replica    replica
	timers     timers
	refused    map[string]bool // logins already reported as refused
}

type timers struct {
	discover, repair, save time.Time
}

// due reports whether every has passed since *last, and if so restarts the wait. Needs m.mu held.
func due(last *time.Time, every time.Duration) bool {
	if time.Since(*last) < every {
		return false
	}
	*last = time.Now()
	return true
}

func New(o Options) *Machine {
	if o.Tick == 0 {
		o.Tick = 3 * time.Second
	}
	if o.StatePath == "" {
		o.StatePath = statePath()
	}
	if o.PeerAddr == nil {
		port := strconv.Itoa(o.Config.Port)
		o.PeerAddr = func(_, ip string) string { return net.JoinHostPort(ip, port) }
	}
	if o.FrontListen == "" {
		o.FrontListen = net.JoinHostPort("127.0.0.1", strconv.Itoa(o.Config.Port))
	}
	transport := &http.Transport{
		Proxy:                 nil, // tailnet traffic never goes through an HTTP proxy from the environment
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	m := &Machine{
		o:   o,
		cfg: o.Config,
		log: o.Log,
		// Every call also carries its own, shorter deadline.
		httpClient: &http.Client{Transport: transport, Timeout: 3 * time.Minute},
		peers:      newPeerView(),
		refused:    map[string]bool{},
	}
	m.forwarder = m.newForwarder(transport)
	return m
}

// Run serves until ctx ends, then hands the accounts to another machine if this one holds them.
func (m *Machine) Run(ctx context.Context) error {
	st, err := loadState(m.o.StatePath)
	if err != nil {
		return fmt.Errorf("state: %w", err)
	}
	m.mu.Lock()
	m.state, m.bootAlive = st, st.LastAlive
	m.mu.Unlock()

	front, err := net.Listen("tcp", m.o.FrontListen)
	if err != nil {
		return fmt.Errorf("can't listen on %s (is the old CLIProxyAPI or another spinup still running?): %w", m.o.FrontListen, err)
	}
	srv := &http.Server{Handler: m.routes(onLocalhost), ReadHeaderTimeout: 30 * time.Second}
	go srv.Serve(front)
	defer srv.Close()
	m.log.Printf("spinup %s on %s (hold: %s)", m.o.Version, m.o.FrontListen, m.cfg.Hold)

	if m.cfg.Hold.CanHold() {
		go m.servePeers(ctx)
		go m.watchSleep(ctx)
	}
	ticker := time.NewTicker(m.o.Tick)
	defer ticker.Stop()
	for {
		m.tick(ctx)
		select {
		case <-ctx.Done():
			m.shutdown()
			return nil
		case <-ticker.C:
		}
	}
}

func (m *Machine) tick(ctx context.Context) {
	ts, err := m.o.Tailnet.Status(ctx)
	if err == nil && ts.Self.Name != "" {
		m.mu.Lock()
		m.self, m.tailnet = ts.Self, ts
		m.mu.Unlock()
	}
	if !m.cfg.Hold.CanHold() {
		m.findLeader(ctx, ts)
		return
	}
	switch {
	case err != nil || !ts.Running:
		m.fenceIfCutOff(fmt.Sprintf("Tailscale status unavailable (%v)", err))
		return
	case !ts.Self.Online:
		m.fenceIfCutOff("this machine is not connected to Tailscale")
		return
	}
	m.connected()
	m.act(ctx, leadership.Decide(m.observe(ctx, ts)), ts)
	m.markAlive()
}

func (m *Machine) act(ctx context.Context, d leadership.Decision, ts tailnet.Status) {
	m.noteWaiting(d)
	switch d.Kind {
	case leadership.Lead:
		m.log.Printf("decision: %v", d)
		if err := m.lead(ctx, d.Epoch); err != nil {
			m.log.Printf("can't lead: %v", err)
		}
	case leadership.StepDown:
		m.log.Printf("decision: %v", d)
		m.stepDown(ctx, d.Leader, d.Epoch, ts)
	case leadership.Follow:
		m.follow(ctx, d.Leader, d.Epoch, ts)
	case leadership.HandOff:
		m.log.Printf("decision: %v", d)
		if err := m.handOff(ctx, d.Target); err != nil {
			m.log.Printf("hand-off to %s failed: %v", d.Target, err)
		}
	}
	if m.isLeading() {
		m.pushIfChanged(ctx)
		m.repair(ctx)
	}
}

func (m *Machine) noteWaiting(d leadership.Decision) {
	m.mu.Lock()
	defer m.mu.Unlock()
	reason := ""
	if d.Kind == leadership.Wait {
		reason = d.Reason
		m.leaderAddr = ""
	}
	if reason != "" && reason != m.waiting {
		m.log.Printf("waiting: %s", reason)
	}
	m.waiting = reason
}

func (m *Machine) isLeading() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.leading
}

func (m *Machine) selfName() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.self.Name
}
