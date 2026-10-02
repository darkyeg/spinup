package service

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/darkyeg/spinup/internal/proxy"
)

// repair takes a newer copy of a refused login from another machine, or says which account to log in again.
func (m *Machine) repair(ctx context.Context) {
	if !m.repairs.due() {
		return
	}
	refused := m.refusedLogins(ctx)
	if len(refused) == 0 {
		return
	}
	replaced := m.takeNewerLogins(ctx)
	for _, name := range refused {
		reported := m.repairs.reported(name)
		switch {
		case slices.Contains(replaced, name):
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
	mu   sync.Mutex
	pace pace
	seen map[string]bool
}

func (r *repairs) due() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pace.due(repairEvery)
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
