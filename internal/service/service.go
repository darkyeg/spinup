// Package service runs spinup's localhost front and library sharing on every machine,
// with leadership, login sync and the peer API on machines that can hold the accounts.
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
	"github.com/darkyeg/spinup/internal/library"
	"github.com/darkyeg/spinup/internal/tailnet"
)

// Proxy is the local CLIProxyAPI; *proxy.Runner is the real one.
type Proxy interface {
	Start(ctx context.Context) error
	// Stop waits a bounded time for quiet (no login being written) before stopping; nil skips the wait.
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
	// HandBackIdle replaces leadership.HandBackIdle.
	HandBackIdle time.Duration

	// Library is shared with the other machines when set; ApplyLibrary installs it after another machine's arrives.
	Library      library.Library
	ApplyLibrary func(context.Context) error
	// LibraryEvery is how often the libraries are compared.
	LibraryEvery time.Duration
}

// Machine is this machine running spinup: it wires the units that each own one part of its state.
type Machine struct {
	o           Options
	cfg         config.Config
	log         *log.Logger
	httpClient  *http.Client
	forwarder   *httputil.ReverseProxy
	stopRun     context.CancelFunc
	serviceDone <-chan struct{}

	transition sync.Mutex // serializes ownership changes and credential imports

	ledger   *ledger
	peers    *peerView
	tail     tailView
	replica  replica
	fence    fence
	notes    outlook
	repairs  repairs
	activity activity
	library  *libraryShare // nil when the library isn't shared
}

func New(o Options) *Machine {
	if o.Tick == 0 {
		o.Tick = 3 * time.Second
	}
	if o.LibraryEvery == 0 {
		o.LibraryEvery = 15 * time.Second
	}
	if o.ApplyLibrary == nil {
		o.ApplyLibrary = func(context.Context) error { return nil }
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
		ledger:     newLedger(o.StatePath),
		peers:      newPeerView(),
		stopRun:    func() {},
	}
	m.forwarder = m.newForwarder(transport)
	return m
}

// Run serves until ctx ends, then hands the accounts to another machine if this one holds them.
func (m *Machine) Run(ctx context.Context) error {
	if err := m.ledger.load(); err != nil {
		return fmt.Errorf("state: %w", err)
	}
	ctx, m.stopRun = context.WithCancel(ctx)
	m.serviceDone = ctx.Done()
	defer m.stopRun()
	m.openLibrary()

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
	if m.library != nil {
		go m.shareLibrary(ctx)
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
		m.tail.set(ts)
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
	m.fence.connected()
	m.act(ctx, leadership.Decide(m.observe(ctx, ts)), ts)
	if m.ledger.touch(time.Now()) {
		m.save()
	}
}

func (m *Machine) act(ctx context.Context, d leadership.Decision, ts tailnet.Status) {
	m.waitFor(d.Waiting())
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
		if err := m.handOff(ctx, d.Target, drainWithin); err != nil {
			m.log.Printf("hand-off to %s failed: %v", d.Target, err)
		}
	}
	if m.ledger.view(time.Now()).Leading {
		m.pushIfChanged(ctx)
		m.repair(ctx)
	}
}

func (m *Machine) waitFor(reason string) {
	if m.notes.note(reason) {
		m.log.Printf("waiting: %s", reason)
	}
}

func (m *Machine) save() {
	if err := m.ledger.save(); err != nil {
		m.log.Printf("save state: %v", err)
	}
}
