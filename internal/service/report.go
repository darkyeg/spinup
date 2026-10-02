package service

import (
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/logins"
)

// Report describes this machine.
func (m *Machine) Report() api.Report {
	now := time.Now()
	st := m.ledger.view(now)
	waiting, leaderAddr := m.notes.get()
	r := api.Report{
		Name: m.tail.selfName(), Hold: m.cfg.Hold, Version: m.o.Version, Leading: st.Leading, Starting: st.Starting,
		LeaderAddr: leaderAddr, Waiting: waiting, Peers: m.peers.peers(), Synced: m.replica.synced(now),
		InFlight: m.activity.inFlight(),
	}
	if m.cfg.Hold.CanHold() {
		r.Epoch, r.Leader = st.Epoch, st.Leader
	}
	if m.o.Proxy != nil {
		r.ProxyRunning = m.o.Proxy.Running()
	}
	if m.cfg.Hold.CanHold() {
		r.Accounts = logins.Summarize(m.cfg.AuthDir)
	}
	return r
}
