package service

import (
	"context"
	"slices"
	"time"

	"github.com/darkyeg/spinup/internal/proxy"
)

// repair runs every 30 seconds on the leader: when the provider refused a login's token, it takes
// a newer copy from another machine if one has it, and otherwise says which account to log in again.
func (m *Machine) repair(ctx context.Context) {
	m.mu.Lock()
	now := due(&m.timers.repair, 30*time.Second)
	m.mu.Unlock()
	if !now {
		return
	}
	refused := m.refusedLogins(ctx)
	if len(refused) == 0 {
		return
	}
	replaced := m.takeNewerLogins(ctx)
	for _, name := range refused {
		m.mu.Lock()
		reported := m.refused[name]
		m.refused[name] = true
		m.mu.Unlock()
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
