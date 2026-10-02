package service

import (
	"slices"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/logins"
)

// Report describes this machine now.
func (m *Machine) Report() api.Report {
	m.mu.Lock()
	r := api.Report{
		Name: m.self.Name, Hold: m.cfg.Hold, Version: m.o.Version, Leading: m.leading,
		LeaderAddr: m.leaderAddr, Waiting: m.waiting, Peers: slices.Clone(m.peers.list),
	}
	if m.cfg.Hold.CanHold() {
		r.Epoch, r.Leader = m.state.Epoch, m.state.Leader
	}
	if !m.replica.at.IsZero() {
		r.Synced = &api.Sync{Epoch: m.replica.epoch, SecondsAgo: int(time.Since(m.replica.at).Seconds())}
	}
	m.mu.Unlock()
	if m.o.Proxy != nil {
		r.ProxyRunning = m.o.Proxy.Running()
	}
	if m.cfg.Hold.CanHold() {
		r.Accounts = logins.Summarize(m.cfg.AuthDir)
	}
	return r
}
