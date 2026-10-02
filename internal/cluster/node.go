package cluster

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/darkyeg/spinup/internal/authsync"
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/proxy"
	"github.com/darkyeg/spinup/internal/tailnet"
)

// Runner starts and stops the local CLIProxyAPI. *proxy.Runner implements it.
type Runner interface {
	Start(ctx context.Context) error
	Stop(quiet func() bool)
	Running() bool
}

// Options configure a Node. Tests replace the tailnet, the proxy and the addresses.
type Options struct {
	Config  config.Config
	Secrets config.Secrets // empty on clients
	Tailnet tailnet.Source
	Runner  Runner // nil on clients
	Log     *log.Logger
	Version string
	// StatePath is state.json (default: in the state folder).
	StatePath string
	// PrepareProxy runs before every proxy start (writes its config).
	PrepareProxy func() error
	// PeerAddr is host:port of a machine's peer API (default: its Tailscale IP and Config.Port).
	PeerAddr func(name, ip string) string
	// FrontListen and PeerListen override where this machine listens (default: 127.0.0.1:Port and
	// the Tailscale IP:Port).
	FrontListen, PeerListen string
	// Tick is how often the machine re-evaluates (default 3s).
	Tick time.Duration
}

// Header names.
const (
	keyHeader  = "X-Spinup-Key"       // the management password, for the peer API
	hopHeader  = "X-Spinup-Forwarded" // set when a request was already forwarded once
	roleHeader = "X-Spinup-Role"      // the caller's role, so machines learn about each other at once
)

// Node is one machine running the spinup service.
type Node struct {
	o    Options
	cfg  config.Config
	log  *log.Logger
	http *http.Client
	fwd  *httputil.ReverseProxy

	transition sync.Mutex // one leadership change at a time

	mu               sync.Mutex
	self             tailnet.Node
	ts               tailnet.Status // last good tailnet status
	st               State
	bootAlive        time.Time // st.LastAlive when the service (re)started or woke up
	isLeader         bool
	leaderAddr       string // peer API of the leader; "" = unknown
	peers            []Peer
	reports          map[string]Report
	offlineSince     map[string]time.Time
	selfOfflineSince time.Time
	syncedEpoch      int64
	syncedAt         time.Time
	lastFP           string
	lastWait         string
	forceLead        bool
	lastPull         time.Time
	lastRepair       time.Time
	lastDiscover     time.Time
	lastSaved        time.Time
	brokenLogged     map[string]bool
}

// New makes a Node.
func New(o Options) *Node {
	if o.Tick == 0 {
		o.Tick = 3 * time.Second
	}
	if o.StatePath == "" {
		o.StatePath = StatePath()
	}
	if o.PeerAddr == nil {
		port := o.Config.Port
		o.PeerAddr = func(_, ip string) string { return net.JoinHostPort(ip, strconv.Itoa(port)) }
	}
	if o.FrontListen == "" {
		o.FrontListen = net.JoinHostPort("127.0.0.1", strconv.Itoa(o.Config.Port))
	}
	tr := &http.Transport{
		Proxy:                 nil, // never send tailnet traffic through an HTTP proxy from the environment
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	n := &Node{
		o:            o,
		cfg:          o.Config,
		log:          o.Log,
		http:         &http.Client{Transport: tr, Timeout: 60 * time.Second},
		reports:      map[string]Report{},
		offlineSince: map[string]time.Time{},
		brokenLogged: map[string]bool{},
	}
	n.fwd = &httputil.ReverseProxy{
		Transport:     tr,
		FlushInterval: -1, // stream answers as they come
		Rewrite: func(pr *httputil.ProxyRequest) {
			t := pr.In.Context().Value(targetKey{}).(target)
			pr.SetURL(t.url)
			pr.Out.Header.Del(hopHeader)
			if t.peer {
				pr.Out.Header.Set(hopHeader, n.selfName())
			}
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			n.log.Printf("forward %s: %v", r.URL.Path, err)
			writeError(w, http.StatusBadGateway, "the machine holding the accounts didn't answer: "+err.Error())
		},
	}
	return n
}

type targetKey struct{}

type target struct {
	url  *url.URL
	peer bool
}

// ---------------------------------------------------------------- running

// Run serves until ctx is cancelled, then hands the accounts off if this machine holds them.
func (n *Node) Run(ctx context.Context) error {
	st, err := LoadState(n.o.StatePath)
	if err != nil {
		return fmt.Errorf("state: %w", err)
	}
	n.mu.Lock()
	n.st, n.bootAlive = st, st.LastAlive
	n.mu.Unlock()

	front, err := net.Listen("tcp", n.o.FrontListen)
	if err != nil {
		return fmt.Errorf("can't listen on %s (is the old CLIProxyAPI or another spinup still running?): %w",
			n.o.FrontListen, err)
	}
	srv := &http.Server{Handler: n.handler(true), ReadHeaderTimeout: 30 * time.Second}
	go srv.Serve(front)
	defer srv.Close()
	n.log.Printf("spinup %s: %s on %s", n.o.Version, n.cfg.Role, n.o.FrontListen)

	if n.cfg.Role.Eligible() {
		go n.servePeers(ctx)
		go n.watchdog(ctx)
	}
	t := time.NewTicker(n.o.Tick)
	defer t.Stop()
	for {
		n.tick(ctx)
		select {
		case <-ctx.Done():
			n.shutdown()
			return nil
		case <-t.C:
		}
	}
}

func (n *Node) servePeers(ctx context.Context) {
	for ctx.Err() == nil {
		addr := n.o.PeerListen
		if addr == "" {
			n.mu.Lock()
			ip := n.self.IP
			n.mu.Unlock()
			if ip == "" {
				time.Sleep(2 * time.Second)
				continue
			}
			addr = net.JoinHostPort(ip, strconv.Itoa(n.cfg.Port))
		}
		l, err := net.Listen("tcp", addr)
		if err != nil {
			n.log.Printf("peer API: can't listen on %s: %v (retrying)", addr, err)
			time.Sleep(10 * time.Second)
			continue
		}
		n.log.Printf("peer API on %s", addr)
		srv := &http.Server{Handler: n.handler(false), ReadHeaderTimeout: 30 * time.Second}
		go func() { <-ctx.Done(); srv.Close() }()
		_ = srv.Serve(l)
	}
}

// watchdog notices when the machine wakes from sleep. A leader that slept may have been replaced
// meanwhile, so it stops using the accounts at once and re-checks before using them again.
func (n *Node) watchdog(ctx context.Context) {
	last := time.Now().Round(0) // wall clock: the monotonic clock may not count sleep
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
		now := time.Now().Round(0)
		gap := now.Sub(last)
		last = now
		if gap < 15*time.Second {
			continue
		}
		n.mu.Lock()
		leader := n.isLeader
		n.bootAlive = n.st.LastAlive
		n.mu.Unlock()
		n.log.Printf("woke up after %s", gap.Round(time.Second))
		if leader {
			n.releaseLocally("woke from sleep; checking that nobody took over")
		}
	}
}

func (n *Node) tick(ctx context.Context) {
	ts, err := n.o.Tailnet.Status(ctx)
	if err == nil && ts.Self.Name != "" {
		n.mu.Lock()
		n.self, n.ts = ts.Self, ts
		n.mu.Unlock()
	}
	if !n.cfg.Role.Eligible() {
		n.findLeaderAsClient(ctx, ts)
		return
	}
	if err != nil || !ts.Running {
		n.selfFence(fmt.Sprintf("Tailscale status unavailable (%v)", err))
		return
	}
	if !ts.Self.Online {
		n.selfFence("this machine is not connected to Tailscale")
		return
	}
	n.mu.Lock()
	n.selfOfflineSince = time.Time{}
	n.mu.Unlock()

	v := n.observe(ctx, ts)
	a := Decide(v)
	n.act(ctx, a, ts)
	n.mu.Lock()
	n.st.LastAlive = time.Now().Round(0)
	save := time.Since(n.lastSaved) > 15*time.Second
	n.mu.Unlock()
	if save {
		n.save()
	}
}

// selfFence: a leader that can't reach Tailscale stops using the accounts well before the others
// would take over (they wait FailoverAfter; this waits half of it).
func (n *Node) selfFence(why string) {
	n.mu.Lock()
	if n.selfOfflineSince.IsZero() {
		n.selfOfflineSince = time.Now()
		n.log.Printf("%s", why)
	}
	long := time.Since(n.selfOfflineSince) >= n.cfg.FailoverAfter()/2
	leader := n.isLeader
	n.mu.Unlock()
	if leader && long {
		n.releaseLocally(why + " for too long; another machine may take over")
	}
}

// releaseLocally stops using the accounts without handing them to anyone. The machine stays the
// known leader, so it resumes if nobody took over.
func (n *Node) releaseLocally(why string) {
	n.transition.Lock()
	defer n.transition.Unlock()
	n.mu.Lock()
	if !n.isLeader {
		n.mu.Unlock()
		return
	}
	n.isLeader = false
	n.mu.Unlock()
	n.o.Runner.Stop(n.quiet)
	n.log.Printf("stopped using the accounts: %s", why)
}

// observe builds the View for Decide.
func (n *Node) observe(ctx context.Context, ts tailnet.Status) View {
	n.mu.Lock()
	members := map[string]Member{}
	for k, v := range n.st.Members {
		members[k] = v
	}
	discover := time.Since(n.lastDiscover) > 10*time.Second
	if discover {
		n.lastDiscover = time.Now()
	}
	n.mu.Unlock()

	// Who to ask: every known hub/standby that is online, plus (every 10s) every other online device.
	ask := map[string]tailnet.Node{}
	for _, p := range ts.Peers {
		if _, known := members[p.Name]; (known || discover) && p.Online {
			ask[p.Name] = p
		}
	}
	reports := n.probeAll(ctx, ask)

	n.mu.Lock()
	defer n.mu.Unlock()
	for name, r := range reports {
		if r.Role.Eligible() {
			n.st.Members[name] = Member{Role: r.Role, Epoch: r.Epoch}
		}
	}
	n.reports = reports
	now := time.Now()
	var peers []Peer
	for name, m := range n.st.Members {
		if name == ts.Self.Name {
			continue
		}
		tn, inTailnet := ts.Peer(name)
		online := inTailnet && tn.Online
		if online {
			delete(n.offlineSince, name)
		} else if _, ok := n.offlineSince[name]; !ok {
			n.offlineSince[name] = now
		}
		p := Peer{Name: name, Role: m.Role, Online: online, Epoch: m.Epoch}
		if !online {
			// Tailscale's last-seen time covers the time before this service started too.
			since := n.offlineSince[name]
			if inTailnet && !tn.LastSeen.IsZero() && tn.LastSeen.Before(since) {
				since = tn.LastSeen
			}
			p.OfflineFor = now.Sub(since)
			p.MayHaveLed = !n.bootAlive.IsZero() && inTailnet &&
				tn.LastSeen.After(n.bootAlive.Add(n.cfg.FailoverAfter()))
		}
		if r, ok := reports[name]; ok {
			p.Reachable, p.IsLeader, p.Epoch = true, r.IsLeader, r.Epoch
			p.Synced = !r.IsLeader && r.SyncedEpoch == n.st.Epoch && r.SyncedSecondsAgo >= 0 && r.SyncedSecondsAgo < 120
		}
		peers = append(peers, p)
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].Name < peers[j].Name })
	n.peers = peers
	force := n.forceLead
	return View{
		Self: ts.Self.Name, Role: n.cfg.Role, IsLeader: n.isLeader, Epoch: n.st.Epoch,
		KnownLeader: n.st.Leader, Peers: peers, FailoverAfter: n.cfg.FailoverAfter(),
		AutoFailback: n.cfg.AutoFailback, ForceLead: force,
	}
}

func (n *Node) act(ctx context.Context, a Action, ts tailnet.Status) {
	n.mu.Lock()
	wait := ""
	if a.Kind == Wait {
		wait = a.Reason
	}
	changed := wait != n.lastWait
	n.lastWait = wait
	n.mu.Unlock()
	if wait != "" && changed {
		n.log.Printf("waiting: %s", wait)
	}

	switch a.Kind {
	case Lead:
		n.log.Printf("decision: %v", a)
		if err := n.becomeLeader(ctx, a.Epoch, true); err != nil {
			n.log.Printf("can't lead: %v", err)
		}
	case StepDown:
		n.log.Printf("decision: %v", a)
		n.stepDown(ctx, a.Leader, a.Epoch, ts)
	case Follow:
		n.follow(ctx, a.Leader, a.Epoch, ts)
	case HandOff:
		n.log.Printf("decision: %v", a)
		if err := n.HandOff(ctx, a.Target); err != nil {
			n.log.Printf("hand-off to %s failed: %v", a.Target, err)
		}
	}

	n.mu.Lock()
	leader := n.isLeader
	n.mu.Unlock()
	if leader {
		n.pushIfChanged(ctx)
		n.repair(ctx)
	} else if a.Kind == Wait && a.Reason != "" {
		n.mu.Lock()
		n.leaderAddr = ""
		n.mu.Unlock()
	}
}

// becomeLeader starts using the accounts with epoch. With gather, it first takes any newer
// logins from the other machines (one of them may have led more recently).
func (n *Node) becomeLeader(ctx context.Context, epoch int64, gather bool) error {
	n.transition.Lock()
	defer n.transition.Unlock()
	return n.becomeLeaderLocked(ctx, epoch, gather)
}

func (n *Node) becomeLeaderLocked(ctx context.Context, epoch int64, gather bool) error {
	if gather {
		for _, p := range n.reachablePeers() {
			if b, err := n.pullBundle(ctx, p); err == nil {
				if res, err := authsync.Apply(n.cfg.AuthDir, n.cfg.RemovedDir(), b.Files, false); err == nil && len(res.Written) > 0 {
					n.log.Printf("took newer logins from %s: %v", p, res.Written)
				}
			}
		}
	}
	if n.o.PrepareProxy != nil {
		if err := n.o.PrepareProxy(); err != nil {
			return err
		}
	}
	startCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if err := n.o.Runner.Start(startCtx); err != nil {
		n.o.Runner.Stop(nil)
		return err
	}
	n.mu.Lock()
	n.isLeader = true
	n.st.Epoch = epoch
	n.st.Leader = n.self.Name
	n.forceLead = false
	n.leaderAddr = ""
	n.lastFP = ""
	n.mu.Unlock()
	n.save()
	n.log.Printf("this machine now holds the accounts (epoch %d)", epoch)
	return nil
}

func (n *Node) stepDown(ctx context.Context, leader string, epoch int64, ts tailnet.Status) {
	n.transition.Lock()
	n.mu.Lock()
	was := n.isLeader
	n.isLeader = false
	n.mu.Unlock()
	if was {
		n.o.Runner.Stop(n.quiet)
		n.log.Printf("stopped using the accounts: %s holds them (epoch %d)", leader, epoch)
	}
	n.transition.Unlock()
	n.follow(ctx, leader, epoch, ts)
}

// follow records leader, sends it our logins once (it keeps whichever copy is newer), and pulls
// its full set every 30 seconds (it also pushes each change).
func (n *Node) follow(ctx context.Context, leader string, epoch int64, ts tailnet.Status) {
	addr := ""
	if p, ok := ts.Peer(leader); ok {
		addr = n.o.PeerAddr(leader, p.IP)
	}
	n.mu.Lock()
	changed := n.st.Leader != leader || n.st.Epoch != epoch
	if changed {
		n.st.Leader = leader
		if epoch > n.st.Epoch {
			n.st.Epoch = epoch
		}
		n.lastPull = time.Time{}
	}
	n.leaderAddr = addr
	pull := time.Since(n.lastPull) > 30*time.Second
	n.mu.Unlock()
	if changed {
		n.log.Printf("following %s (epoch %d)", leader, epoch)
		n.save()
		if err := n.pushBundle(ctx, leader, false); err != nil {
			n.log.Printf("send logins to %s: %v", leader, err)
		}
	}
	if pull && addr != "" {
		n.pullFromLeader(ctx, leader)
	}
}

func (n *Node) pullFromLeader(ctx context.Context, leader string) {
	b, err := n.pullBundle(ctx, leader)
	if err != nil {
		n.log.Printf("sync from %s: %v", leader, err)
		return
	}
	n.applyFromLeader(b)
}

func (n *Node) applyFromLeader(b authsync.Bundle) {
	res, err := authsync.Apply(n.cfg.AuthDir, n.cfg.RemovedDir(), b.Files, b.Full)
	if err != nil {
		n.log.Printf("sync from %s: %v", b.From, err)
		return
	}
	if len(res.Written)+len(res.Removed) > 0 {
		n.log.Printf("synced from %s: updated %v, removed %v", b.From, res.Written, res.Removed)
	}
	n.mu.Lock()
	n.lastPull = time.Now()
	if b.Full {
		n.syncedEpoch, n.syncedAt = b.Epoch, time.Now()
	}
	n.mu.Unlock()
}

// pushIfChanged sends the logins to every follower as soon as the proxy refreshes one.
func (n *Node) pushIfChanged(ctx context.Context) {
	fp := authsync.Fingerprint(n.cfg.AuthDir)
	n.mu.Lock()
	same := fp == n.lastFP
	n.lastFP = fp
	n.mu.Unlock()
	if same {
		return
	}
	for _, p := range n.reachablePeers() {
		if err := n.pushBundle(ctx, p, true); err != nil {
			n.log.Printf("push logins to %s: %v", p, err)
		}
	}
}

// repair: when the proxy reports a login whose token was refused, take a newer copy from another
// machine if one has it; otherwise tell the user to log that account in again.
func (n *Node) repair(ctx context.Context) {
	n.mu.Lock()
	due := time.Since(n.lastRepair) > 30*time.Second
	if due {
		n.lastRepair = time.Now()
	}
	n.mu.Unlock()
	if !due || n.o.Secrets.ManagementPassword == "" {
		return
	}
	states, err := proxy.AuthStates(ctx, n.cfg.ProxyPort, n.o.Secrets.ManagementPassword)
	if err != nil {
		return
	}
	var broken []string
	for _, s := range states {
		if s.Broken() {
			broken = append(broken, s.Name)
		}
	}
	if len(broken) == 0 {
		return
	}
	fixed := map[string]bool{}
	for _, p := range n.reachablePeers() {
		b, err := n.pullBundle(ctx, p)
		if err != nil {
			continue
		}
		res, err := authsync.Apply(n.cfg.AuthDir, n.cfg.RemovedDir(), b.Files, false)
		if err != nil {
			continue
		}
		for _, w := range res.Written {
			fixed[w] = true
		}
	}
	for _, name := range broken {
		n.mu.Lock()
		logged := n.brokenLogged[name]
		n.brokenLogged[name] = true
		n.mu.Unlock()
		switch {
		case fixed[name]:
			n.log.Printf("login %s was refused; took a newer copy from another machine", name)
		case !logged:
			n.log.Printf("login %s was refused and no machine has a newer copy: log that account in again in the dashboard", name)
		}
	}
}

// HandOff moves the accounts to target, the planned way: this machine stops the proxy first,
// sends its final logins, and target starts only after it has them.
func (n *Node) HandOff(ctx context.Context, targetName string) error {
	n.transition.Lock()
	defer n.transition.Unlock()
	n.mu.Lock()
	leader, epoch := n.isLeader, n.st.Epoch
	_, reachable := n.reports[targetName]
	n.mu.Unlock()
	if !leader {
		return errors.New("this machine doesn't hold the accounts")
	}
	if targetName == n.selfName() {
		return nil
	}
	if !reachable {
		return fmt.Errorf("%s isn't reachable or isn't a hub/standby", targetName)
	}
	n.mu.Lock()
	n.isLeader = false
	n.mu.Unlock()
	n.o.Runner.Stop(n.quiet)
	files, err := authsync.Read(n.cfg.AuthDir)
	if err == nil {
		b := authsync.Bundle{From: n.selfName(), Epoch: epoch + 1, Full: true, Files: files}
		err = n.call(ctx, targetName, http.MethodPost, "/spinup/takeover", b, nil, 90*time.Second)
	}
	if err != nil {
		n.log.Printf("hand-off to %s failed (%v); resuming", targetName, err)
		if err2 := n.becomeLeaderLocked(ctx, epoch, false); err2 != nil {
			return fmt.Errorf("%v; and couldn't resume: %v", err, err2)
		}
		return err
	}
	n.mu.Lock()
	n.st.Leader, n.st.Epoch = targetName, epoch+1
	n.leaderAddr = ""
	n.lastPull = time.Time{}
	n.mu.Unlock()
	n.save()
	n.log.Printf("handed the accounts to %s (epoch %d)", targetName, epoch+1)
	return nil
}

func (n *Node) takeover(ctx context.Context, b authsync.Bundle) error {
	n.transition.Lock()
	defer n.transition.Unlock()
	n.mu.Lock()
	leader, epoch := n.isLeader, n.st.Epoch
	n.mu.Unlock()
	if leader {
		return errors.New("already holding the accounts")
	}
	if b.Epoch <= epoch {
		return fmt.Errorf("stale hand-off (epoch %d, this machine is at %d)", b.Epoch, epoch)
	}
	if _, err := authsync.Apply(n.cfg.AuthDir, n.cfg.RemovedDir(), b.Files, true); err != nil {
		return err
	}
	n.log.Printf("taking over the accounts from %s", b.From)
	return n.becomeLeaderLocked(ctx, b.Epoch, false)
}

func (n *Node) shutdown() {
	n.mu.Lock()
	leader := n.isLeader
	n.mu.Unlock()
	if leader {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if target := n.handOffTarget(ctx); target != "" {
			if err := n.HandOff(ctx, target); err == nil {
				leader = false
			}
		}
	}
	if leader {
		n.releaseLocally("service stopping and no synced machine to hand the accounts to")
	}
	n.mu.Lock()
	n.st.LastAlive = time.Now().Round(0)
	n.mu.Unlock()
	n.save()
}

// handOffTarget asks the other hub/standby machines right now and picks a synced one (the hub
// first), so a planned stop never drops the accounts when another machine can take them.
func (n *Node) handOffTarget(ctx context.Context) string {
	n.mu.Lock()
	ask := map[string]tailnet.Node{}
	for name := range n.st.Members {
		if p, ok := n.ts.Peer(name); ok && p.Online {
			ask[name] = p
		}
	}
	epoch := n.st.Epoch
	n.mu.Unlock()
	best, bestHub := "", false
	for name, r := range n.probeAll(ctx, ask) {
		synced := r.Role.Eligible() && !r.IsLeader && r.SyncedEpoch == epoch && r.SyncedSecondsAgo >= 0 && r.SyncedSecondsAgo < 120
		isHub := r.Role == config.RoleHub
		if synced && (best == "" || (isHub && !bestHub) || (isHub == bestHub && name < best)) {
			best, bestHub = name, isHub
			n.mu.Lock()
			n.reports[name] = r
			n.mu.Unlock()
		}
	}
	return best
}

// quiet reports whether no login changed in the last second (so a refresh isn't mid-write).
func (n *Node) quiet() bool {
	a := authsync.Fingerprint(n.cfg.AuthDir)
	time.Sleep(time.Second)
	return a == authsync.Fingerprint(n.cfg.AuthDir)
}

func (n *Node) save() {
	n.mu.Lock()
	st := n.st
	st.Members = map[string]Member{}
	for k, v := range n.st.Members {
		st.Members[k] = v
	}
	n.lastSaved = time.Now()
	n.mu.Unlock()
	if err := SaveState(n.o.StatePath, st); err != nil {
		n.log.Printf("save state: %v", err)
	}
}

// learn remembers a hub/standby that called us (callers authenticate with the key first).
func (n *Node) learn(name string, role config.Role) {
	if name == "" || !role.Eligible() {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, ok := n.st.Members[name]; !ok && name != n.self.Name {
		n.st.Members[name] = Member{Role: role}
		n.log.Printf("met %s (%s)", name, role)
	}
}

func (n *Node) selfName() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.self.Name
}

func (n *Node) reachablePeers() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []string
	for name, r := range n.reports {
		if r.Role.Eligible() {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------- clients

// findLeaderAsClient asks every online device who holds the accounts. Before any machine runs
// the service, a plain CLIProxyAPI hub (set up by spinup.py) is used directly.
func (n *Node) findLeaderAsClient(ctx context.Context, ts tailnet.Status) {
	type answer struct {
		addr  string
		epoch int64
		ok    bool
	}
	ch := make(chan answer)
	count := 0
	for _, p := range ts.Peers {
		if !p.Online {
			continue
		}
		count++
		go func(p tailnet.Node) {
			addr := n.o.PeerAddr(p.Name, p.IP)
			var li LeaderInfo
			if err := n.get(ctx, "http://"+addr+"/spinup/leader", &li, 2*time.Second); err == nil {
				ch <- answer{addr, li.Epoch, li.IsLeader}
				return
			}
			legacy := proxy.Healthy(ctx, "http://"+addr)
			ch <- answer{addr, -1, legacy}
		}(p)
	}
	best := answer{epoch: -2}
	for i := 0; i < count; i++ {
		if a := <-ch; a.ok && a.epoch > best.epoch {
			best = a
		}
	}
	n.mu.Lock()
	if best.ok {
		n.leaderAddr = best.addr
	} else {
		n.leaderAddr = ""
	}
	n.mu.Unlock()
}

// ---------------------------------------------------------------- peer calls

func (n *Node) probeAll(ctx context.Context, ask map[string]tailnet.Node) map[string]Report {
	type res struct {
		name string
		r    Report
		err  error
	}
	ch := make(chan res)
	for name, p := range ask {
		go func(name, ip string) {
			var r Report
			err := n.getKeyed(ctx, n.o.PeerAddr(name, ip), "/spinup/state", &r, 2*time.Second)
			if err == nil && r.Name != name {
				err = fmt.Errorf("answered as %q", r.Name)
			}
			ch <- res{name, r, err}
		}(name, p.IP)
	}
	out := map[string]Report{}
	for range ask {
		if x := <-ch; x.err == nil {
			out[x.name] = x.r
		}
	}
	return out
}

func (n *Node) addrOf(name string) (string, error) {
	n.mu.Lock()
	ts := n.ts
	n.mu.Unlock()
	p, ok := ts.Peer(name)
	if !ok {
		return "", fmt.Errorf("%s is not on the tailnet", name)
	}
	return n.o.PeerAddr(name, p.IP), nil
}

func (n *Node) pullBundle(ctx context.Context, name string) (authsync.Bundle, error) {
	var b authsync.Bundle
	err := n.call(ctx, name, http.MethodGet, "/spinup/auth", nil, &b, 30*time.Second)
	return b, err
}

func (n *Node) pushBundle(ctx context.Context, name string, full bool) error {
	files, err := authsync.Read(n.cfg.AuthDir)
	if err != nil {
		return err
	}
	n.mu.Lock()
	b := authsync.Bundle{From: n.self.Name, Epoch: n.st.Epoch, Full: full, Files: files}
	n.mu.Unlock()
	return n.call(ctx, name, http.MethodPost, "/spinup/auth", b, nil, 30*time.Second)
}

func (n *Node) call(ctx context.Context, name, method, path string, body, out any, timeout time.Duration) error {
	addr, err := n.addrOf(name)
	if err != nil {
		return err
	}
	return n.do(ctx, method, "http://"+addr+path, body, out, timeout)
}

func (n *Node) getKeyed(ctx context.Context, addr, path string, out any, timeout time.Duration) error {
	return n.do(ctx, http.MethodGet, "http://"+addr+path, nil, out, timeout)
}

func (n *Node) get(ctx context.Context, u string, out any, timeout time.Duration) error {
	return n.do(ctx, http.MethodGet, u, nil, out, timeout)
}

func (n *Node) do(ctx context.Context, method, u string, body, out any, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if n.o.Secrets.ManagementPassword != "" {
		req.Header.Set(keyHeader, n.o.Secrets.ManagementPassword)
	}
	req.Header.Set(hopHeader, n.selfName()) // a machine that receives this never forwards it again
	req.Header.Set(roleHeader, string(n.cfg.Role))
	resp, err := n.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s %s: %s %s", method, u, resp.Status, bytes.TrimSpace(msg))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// ---------------------------------------------------------------- HTTP API

// LeaderInfo is public: it says who holds the accounts, nothing secret.
type LeaderInfo struct {
	Name     string `json:"name"`
	IsLeader bool   `json:"is_leader"`
	Leader   string `json:"leader"`
	Epoch    int64  `json:"epoch"`
}

// Report is a machine's state, for other machines and `spinup status`.
type Report struct {
	Name             string             `json:"name"`
	Role             config.Role        `json:"role"`
	Version          string             `json:"version"`
	IsLeader         bool               `json:"is_leader"`
	Epoch            int64              `json:"epoch"`
	Leader           string             `json:"leader"`
	LeaderAddr       string             `json:"leader_addr,omitempty"`
	SyncedEpoch      int64              `json:"synced_epoch"`
	SyncedSecondsAgo int                `json:"synced_seconds_ago"`
	ProxyRunning     bool               `json:"proxy_running"`
	Waiting          string             `json:"waiting,omitempty"`
	Accounts         []authsync.Summary `json:"accounts,omitempty"`
	Peers            []Peer             `json:"peers,omitempty"`
}

// Report describes this machine now.
func (n *Node) Report() Report {
	n.mu.Lock()
	defer n.mu.Unlock()
	r := Report{
		Name: n.self.Name, Role: n.cfg.Role, Version: n.o.Version, IsLeader: n.isLeader,
		Epoch: n.st.Epoch, Leader: n.st.Leader, LeaderAddr: n.leaderAddr, SyncedEpoch: n.syncedEpoch,
		SyncedSecondsAgo: -1, Waiting: n.lastWait, Peers: append([]Peer(nil), n.peers...),
	}
	if !n.cfg.Role.Eligible() {
		r.Leader, r.Epoch = "", 0
	}
	if !n.syncedAt.IsZero() {
		r.SyncedSecondsAgo = int(time.Since(n.syncedAt).Seconds())
	}
	if n.o.Runner != nil {
		r.ProxyRunning = n.o.Runner.Running()
	}
	if n.cfg.Role.Eligible() {
		r.Accounts = authsync.Summarize(n.cfg.AuthDir)
	}
	return r
}

func (n *Node) authorized(r *http.Request) bool {
	want := n.o.Secrets.ManagementPassword
	got := r.Header.Get(keyHeader)
	return want != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (n *Node) handler(local bool) http.Handler {
	mux := http.NewServeMux()
	keyed := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !n.authorized(r) {
				writeError(w, http.StatusUnauthorized, "missing or wrong "+keyHeader)
				return
			}
			n.learn(r.Header.Get(hopHeader), config.Role(r.Header.Get(roleHeader)))
			h(w, r)
		}
	}
	mux.HandleFunc("GET /spinup/leader", func(w http.ResponseWriter, r *http.Request) {
		n.mu.Lock()
		li := LeaderInfo{Name: n.self.Name, IsLeader: n.isLeader, Leader: n.st.Leader, Epoch: n.st.Epoch}
		n.mu.Unlock()
		writeJSON(w, li)
	})
	state := func(w http.ResponseWriter, r *http.Request) { writeJSON(w, n.Report()) }
	if local {
		mux.HandleFunc("GET /spinup/state", state) // this machine only; nothing secret in it
	} else {
		mux.HandleFunc("GET /spinup/state", keyed(state))
	}
	mux.HandleFunc("GET /spinup/auth", keyed(func(w http.ResponseWriter, r *http.Request) {
		if !n.cfg.Role.Eligible() {
			writeError(w, http.StatusNotFound, "clients hold no logins")
			return
		}
		files, err := authsync.Read(n.cfg.AuthDir)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		n.mu.Lock()
		b := authsync.Bundle{From: n.self.Name, Epoch: n.st.Epoch, Full: true, Files: files}
		n.mu.Unlock()
		writeJSON(w, b)
	}))
	mux.HandleFunc("POST /spinup/auth", keyed(func(w http.ResponseWriter, r *http.Request) {
		var b authsync.Bundle
		if !n.cfg.Role.Eligible() || !decode(w, r, &b) {
			return
		}
		n.mu.Lock()
		fromLeader := !n.isLeader && b.From == n.st.Leader && b.Epoch >= n.st.Epoch
		n.mu.Unlock()
		if fromLeader {
			n.applyFromLeader(b)
		} else if _, err := authsync.Apply(n.cfg.AuthDir, n.cfg.RemovedDir(), b.Files, false); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("GET /spinup/secrets", keyed(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, n.o.Secrets)
	}))
	mux.HandleFunc("POST /spinup/takeover", keyed(func(w http.ResponseWriter, r *http.Request) {
		var b authsync.Bundle
		if !n.cfg.Role.Eligible() || !decode(w, r, &b) {
			return
		}
		if err := n.takeover(r.Context(), b); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /spinup/handoff", keyed(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			To string `json:"to"`
		}
		if !decode(w, r, &req) {
			return
		}
		n.mu.Lock()
		leader, known := n.isLeader, n.st.Leader
		n.mu.Unlock()
		var err error
		switch {
		case leader:
			err = n.HandOff(r.Context(), req.To)
		case known != "" && r.Header.Get(hopHeader) == "":
			err = n.call(r.Context(), known, http.MethodPost, "/spinup/handoff", req, nil, 2*time.Minute)
		default:
			err = errors.New("no machine holds the accounts right now")
		}
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /spinup/force-lead", keyed(func(w http.ResponseWriter, r *http.Request) {
		if !n.cfg.Role.Eligible() {
			writeError(w, http.StatusBadRequest, "clients can't hold the accounts")
			return
		}
		n.mu.Lock()
		n.forceLead = true
		n.mu.Unlock()
		n.log.Printf("the user asked this machine to take the accounts (lead --force)")
		writeJSON(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("/", n.forward)
	return mux
}

// forward sends everything else (the API, the dashboard) to whoever holds the accounts.
func (n *Node) forward(w http.ResponseWriter, r *http.Request) {
	n.mu.Lock()
	leader, addr := n.isLeader, n.leaderAddr
	n.mu.Unlock()
	var t target
	switch {
	case leader:
		t.url = &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(n.cfg.ProxyPort))}
	case r.Header.Get(hopHeader) != "":
		writeError(w, http.StatusServiceUnavailable, "this machine doesn't hold the accounts any more; retry")
		return
	case addr == "":
		writeError(w, http.StatusServiceUnavailable,
			"no machine holds the accounts right now (run `spinup status` to see why)")
		return
	default:
		t = target{url: &url.URL{Scheme: "http", Host: addr}, peer: true}
	}
	n.fwd.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), targetKey{}, t)))
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<20)).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad request: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "spinup: " + msg, "type": "spinup"}})
}
