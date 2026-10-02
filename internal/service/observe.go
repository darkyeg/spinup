package service

import (
	"cmp"
	"context"
	"fmt"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/leadership"
	"github.com/darkyeg/spinup/internal/tailnet"
)

// observe asks the members, and now and then every other online device, and describes them for leadership.Decide.
func (m *Machine) observe(ctx context.Context, ts tailnet.Status) leadership.View {
	members := m.ledger.members()
	discover := m.peers.discoverDue()
	candidates := map[string]tailnet.Node{}
	for _, p := range ts.Peers {
		if _, isMember := members[p.Name]; p.Online && (isMember || discover) {
			candidates[p.Name] = p
		}
	}
	answered := m.probe(ctx, candidates)
	m.ledger.recordMembers(asMembers(answered))

	now := time.Now()
	st := m.ledger.view(now)
	peers := m.peers.see(sighting{
		members: m.ledger.members(), tailnet: ts, answered: answered, epoch: st.Epoch,
		bootAlive: st.BootAlive, failover: m.cfg.FailoverAfter(), now: now,
	})
	return leadership.View{
		Self: ts.Self.Name, Hold: m.cfg.Hold, Leading: st.claiming(), Epoch: st.Epoch,
		KnownLeader: st.Leader, Peers: peers, FailoverAfter: m.cfg.FailoverAfter(),
		AutoFailback: m.cfg.AutoFailback, Activity: m.activity.snapshot(now), IdleBeforeHandBack: m.handBackIdle(),
		Forced: st.Forced, Pending: pendingHandOff(st, now, m.handOffWindow()),
	}
}

// pendingHandOff is set while the leader this machine knows is a hand-off target it never heard back from.
func pendingHandOff(st standing, now time.Time, window time.Duration) *leadership.PendingHandOff {
	if st.HandOff == nil || st.HandOff.Target != st.Leader {
		return nil
	}
	return &leadership.PendingHandOff{Age: now.Sub(st.HandOff.At), Window: window}
}

// handOffWindow is how long a request that timed out could still reach its machine.
func (m *Machine) handOffWindow() time.Duration { return 3 * m.o.Tick }

func (m *Machine) handBackIdle() time.Duration {
	return cmp.Or(m.o.HandBackIdle, leadership.HandBackIdle)
}

// probe returns the reports of devices that prove they know the password; the key goes only to them.
func (m *Machine) probe(ctx context.Context, devices map[string]tailnet.Node) map[string]api.Report {
	holders := map[string]tailnet.Node{}
	for name, l := range askAll(ctx, devices, m.askLeader) {
		if l.Name == name && l.Hold.CanHold() && l.Proof == api.KnowsPassword {
			holders[name] = devices[name]
		}
	}
	return askAll(ctx, holders, func(ctx context.Context, name string, node tailnet.Node) (api.Report, error) {
		var r api.Report
		err := m.client().Get(ctx, m.peerURL(name, node.IP, api.PathState), &r)
		if err == nil && r.Name != name {
			err = fmt.Errorf("answered as %q", r.Name)
		}
		return r, err
	})
}

type provenLeader struct {
	api.Leader
	Proof api.Proof
}

func (m *Machine) askLeader(ctx context.Context, name string, node tailnet.Node) (provenLeader, error) {
	l, proof, err := m.publicClient().AskLeader(ctx, "http://"+m.o.PeerAddr(name, node.IP), name, m.o.Secrets)
	return provenLeader{l, proof}, err
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

// findLeader follows the highest epoch among devices that lead and prove they know the API key.
func (m *Machine) findLeader(ctx context.Context, ts tailnet.Status) {
	online := map[string]tailnet.Node{}
	for _, p := range ts.Peers {
		if p.Online {
			online[p.Name] = p
		}
	}
	addr, best := "", int64(-1)
	for name, l := range askAll(ctx, online, m.askLeader) {
		if l.Leading && l.Name == name && l.Proof >= api.KnowsAPIKey && l.Epoch > best {
			addr, best = m.o.PeerAddr(name, online[name].IP), l.Epoch
		}
	}
	m.notes.routeTo(addr)
}

// learn remembers a machine that can hold and called with the key.
func (m *Machine) learn(name string, hold config.Hold) {
	if name == "" || !hold.CanHold() {
		return
	}
	if m.ledger.meet(name, hold, m.tail.selfName()) {
		m.log.Printf("met %s (%s)", name, hold)
	}
}
