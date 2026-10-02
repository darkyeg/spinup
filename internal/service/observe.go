package service

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/leadership"
	"github.com/darkyeg/spinup/internal/tailnet"
)

// syncedWithin: a copy older than this doesn't count as synced.
const syncedWithin = 2 * time.Minute

// peerView is what this machine saw of the others at the last look.
type peerView struct {
	list         []leadership.Peer
	answered     map[string]api.Report
	offlineSince map[string]time.Time
}

func newPeerView() peerView {
	return peerView{answered: map[string]api.Report{}, offlineSince: map[string]time.Time{}}
}

// observe asks every known member, and every ten seconds every other online device, then
// describes them for leadership.Decide.
func (m *Machine) observe(ctx context.Context, ts tailnet.Status) leadership.View {
	m.mu.Lock()
	members := maps.Clone(m.state.Members)
	discover := due(&m.timers.discover, 10*time.Second)
	m.mu.Unlock()

	known, unknown := map[string]tailnet.Node{}, map[string]tailnet.Node{}
	for _, p := range ts.Peers {
		_, isMember := members[p.Name]
		switch {
		case !p.Online:
		case isMember:
			known[p.Name] = p
		case discover:
			unknown[p.Name] = p
		}
	}
	maps.Copy(known, m.holdersAmong(ctx, unknown))
	answered := m.probe(ctx, known)

	m.mu.Lock()
	defer m.mu.Unlock()
	for name, r := range answered {
		if r.Hold.CanHold() {
			m.state.Members[name] = member{Hold: r.Hold, Epoch: r.Epoch}
		}
	}
	m.peers.answered = answered
	m.peers.list = m.describePeers(ts, answered)
	return leadership.View{
		Self: ts.Self.Name, Hold: m.cfg.Hold, Leading: m.leading, Epoch: m.state.Epoch,
		KnownLeader: m.state.Leader, Peers: m.peers.list, FailoverAfter: m.cfg.FailoverAfter(),
		AutoFailback: m.cfg.AutoFailback, Forced: m.forced,
	}
}

// describePeers needs m.mu held.
func (m *Machine) describePeers(ts tailnet.Status, answered map[string]api.Report) []leadership.Peer {
	now := time.Now()
	var peers []leadership.Peer
	for name, mem := range m.state.Members {
		if name == ts.Self.Name {
			continue
		}
		p := leadership.Peer{Name: name, Hold: mem.Hold, Epoch: mem.Epoch}
		node, inTailnet := ts.Peer(name)
		switch r, ok := answered[name]; {
		case ok:
			p.State, p.Epoch = peerState(r, m.state.Epoch), r.Epoch
		case inTailnet && node.Online:
			p.State = leadership.Silent
		default:
			p.State = leadership.Offline
		}
		if p.State == leadership.Offline {
			p.OfflineFor, p.MayHaveLed = m.offlineFacts(name, node, inTailnet, now)
		} else {
			delete(m.peers.offlineSince, name)
		}
		peers = append(peers, p)
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].Name < peers[j].Name })
	return peers
}

// offlineFacts counts from Tailscale's last sight, which may predate this service's start.
func (m *Machine) offlineFacts(name string, node tailnet.Node, inTailnet bool, now time.Time) (time.Duration, bool) {
	since, seen := m.peers.offlineSince[name]
	if !seen {
		since = now
		m.peers.offlineSince[name] = now
	}
	if inTailnet && !node.LastSeen.IsZero() && node.LastSeen.Before(since) {
		since = node.LastSeen
	}
	mayHaveLed := !m.bootAlive.IsZero() && inTailnet && node.LastSeen.After(m.bootAlive.Add(m.cfg.FailoverAfter()))
	return now.Sub(since), mayHaveLed
}

func peerState(r api.Report, epoch int64) leadership.PeerState {
	switch {
	case r.Leading:
		return leadership.Leading
	case r.Synced != nil && r.Synced.Epoch == epoch && r.Synced.SecondsAgo >= 0 &&
		time.Duration(r.Synced.SecondsAgo)*time.Second < syncedWithin:
		return leadership.Synced
	}
	return leadership.Standing
}

// holdersAmong asks devices publicly whether they run spinup and can hold the accounts, so the
// key only ever goes to those.
func (m *Machine) holdersAmong(ctx context.Context, devices map[string]tailnet.Node) map[string]tailnet.Node {
	holders := map[string]tailnet.Node{}
	for name, l := range askAll(ctx, devices, m.askLeader) {
		if l.Name == name && l.Hold.CanHold() {
			holders[name] = devices[name]
		}
	}
	return holders
}

// probe asks members for their reports; only answers given under the expected name count.
func (m *Machine) probe(ctx context.Context, members map[string]tailnet.Node) map[string]api.Report {
	reports := askAll(ctx, members, func(ctx context.Context, name string, node tailnet.Node) (api.Report, error) {
		var r api.Report
		err := m.client().Get(ctx, m.peerURL(name, node.IP, api.PathState), &r)
		if err == nil && r.Name != name {
			err = fmt.Errorf("answered as %q", r.Name)
		}
		return r, err
	})
	return reports
}

func (m *Machine) askLeader(ctx context.Context, name string, node tailnet.Node) (api.Leader, error) {
	var l api.Leader
	err := m.publicClient().Get(ctx, m.peerURL(name, node.IP, api.PathLeader), &l)
	return l, err
}

// askAll asks every device at once, two seconds each, and keeps the answers.
func askAll[T any](ctx context.Context, devices map[string]tailnet.Node, ask func(context.Context, string, tailnet.Node) (T, error)) map[string]T {
	type answer struct {
		name  string
		value T
		err   error
	}
	answers := make(chan answer, len(devices))
	for name, node := range devices {
		go func() {
			ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			v, err := ask(ctx, name, node)
			answers <- answer{name, v, err}
		}()
	}
	out := map[string]T{}
	for range devices {
		if a := <-answers; a.err == nil {
			out[a.name] = a.value
		}
	}
	return out
}

// findLeader is how a machine that never holds finds the accounts: it asks every online device
// and follows the highest epoch that says it leads.
func (m *Machine) findLeader(ctx context.Context, ts tailnet.Status) {
	online := map[string]tailnet.Node{}
	for _, p := range ts.Peers {
		if p.Online {
			online[p.Name] = p
		}
	}
	addr, best := "", int64(-1)
	for name, l := range askAll(ctx, online, m.askLeader) {
		if l.Leading && l.Name == name && l.Epoch > best {
			addr, best = m.o.PeerAddr(name, online[name].IP), l.Epoch
		}
	}
	m.mu.Lock()
	m.leaderAddr = addr
	m.mu.Unlock()
}

// learn remembers a machine that can hold and called with the key.
func (m *Machine) learn(name string, hold config.Hold) {
	if name == "" || !hold.CanHold() {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, known := m.state.Members[name]; !known && name != m.self.Name {
		m.state.Members[name] = member{Hold: hold}
		m.log.Printf("met %s (%s)", name, hold)
	}
}
