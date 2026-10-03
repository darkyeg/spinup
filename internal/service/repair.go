package service

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/proxy"
)

// repair takes a newer copy of a refused login from another machine, or says which account to log in again.
func (m *Machine) repair(ctx context.Context) {
	if !m.repairs.begin() {
		return
	}
	go func() {
		defer m.repairs.finish()
		m.repairNow(ctx)
	}()
}

func (m *Machine) repairNow(ctx context.Context) {
	refused := m.refusedLogins(ctx)
	if len(refused) == 0 {
		return
	}
	merged, err := m.mergeLogins(ctx, api.Logins{Files: m.collectLogins(ctx)})
	if err != nil {
		m.log.Printf("login repair deferred: %v", err)
		return
	}
	for _, name := range refused {
		reported := m.repairs.reported(name)
		switch {
		case slices.Contains(merged.Written, name):
			m.log.Printf("login %s was refused; took a newer copy from another machine", name)
		case !reported:
			m.log.Printf("login %s was refused and no machine has a newer copy: log that account in again in the dashboard", name)
		}
	}
}

func (m *Machine) refusedLogins(ctx context.Context) []string {
	states, err := proxy.LoginStates(ctx, m.cfg.ProxyPort, m.o.Secrets.ManagementPassword)
	if err != nil {
		return nil
	}
	var refused []string
	for _, s := range states {
		if s.Refused() {
			refused = append(refused, s.Name)
		}
	}
	return refused
}

const repairEvery = 30 * time.Second

// repairs paces the repair look and remembers which refused logins were already reported.
type repairs struct {
	mu      sync.Mutex
	pace    pace
	seen    map[string]bool
	running bool
}

func (r *repairs) begin() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running || !r.pace.due(repairEvery) {
		return false
	}
	r.running = true
	return true
}

func (r *repairs) finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running = false
}

// reported says whether name was reported before, and counts it as reported now.
func (r *repairs) reported(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen == nil {
		r.seen = map[string]bool{}
	}
	was := r.seen[name]
	r.seen[name] = true
	return was
}
