package service

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/leadership"
	"github.com/darkyeg/spinup/internal/logins"
	"github.com/darkyeg/spinup/internal/tailnet"
)

// lead takes any newer logins from the other machines first: one of them may have led more
// recently. The decision was made on a snapshot, so it is checked again before the proxy starts.
func (m *Machine) lead(ctx context.Context, epoch int64) error {
	m.transition.Lock()
	defer m.transition.Unlock()
	if written := m.takeNewerLogins(ctx); len(written) > 0 {
		m.log.Printf("took newer logins: %v", written)
	}
	if err := m.stillUnheld(ctx, epoch); err != nil {
		return err
	}
	return m.startLeading(ctx, epoch)
}

// stillUnheld fails when this machine leads already, or another machine started leading since
// the decision. Needs m.transition held.
func (m *Machine) stillUnheld(ctx context.Context, epoch int64) error {
	m.mu.Lock()
	leading, current := m.leading, m.state.Epoch
	m.mu.Unlock()
	switch {
	case leading:
		return errors.New("already holding the accounts")
	case current >= epoch:
		return fmt.Errorf("epoch %d was overtaken by %d", epoch, current)
	}
	for _, peer := range m.answeringMembers() {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		var l api.Leader
		err := m.getPeer(ctx, peer, api.PathLeader, &l)
		cancel()
		if err == nil && l.Leading {
			return fmt.Errorf("%s holds the accounts now", peer)
		}
	}
	return nil
}

// startLeading needs m.transition held.
func (m *Machine) startLeading(ctx context.Context, epoch int64) error {
	if m.o.PrepareProxy != nil {
		if err := m.o.PrepareProxy(); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if err := m.o.Proxy.Start(ctx); err != nil {
		m.o.Proxy.Stop(nil)
		return err
	}
	m.mu.Lock()
	m.leading, m.forced, m.leaderAddr = true, false, ""
	m.state.Epoch, m.state.Leader = epoch, m.self.Name
	m.replica.pushed = ""
	m.mu.Unlock()
	m.save()
	m.log.Printf("this machine now holds the accounts (epoch %d)", epoch)
	return nil
}

// stepDown stops at once: another machine already runs the accounts, so waiting for a quiet
// moment would only let both refresh.
func (m *Machine) stepDown(ctx context.Context, leader string, epoch int64, ts tailnet.Status) {
	m.transition.Lock()
	m.mu.Lock()
	was := m.leading
	m.leading = false
	m.mu.Unlock()
	if was {
		m.o.Proxy.Stop(nil)
		m.log.Printf("stopped using the accounts: %s holds them (epoch %d)", leader, epoch)
	}
	m.transition.Unlock()
	m.follow(ctx, leader, epoch, ts)
}

// releaseLocally stops using the accounts without handing them on. The machine stays the known
// leader, so it resumes if nobody took over meanwhile.
func (m *Machine) releaseLocally(why string) {
	m.transition.Lock()
	defer m.transition.Unlock()
	m.mu.Lock()
	was := m.leading
	m.leading = false
	m.mu.Unlock()
	if was {
		m.o.Proxy.Stop(m.quiet)
		m.log.Printf("stopped using the accounts: %s", why)
	}
}

// fenceIfCutOff: a leader cut off from Tailscale stops at half of FailoverAfter, well before
// another machine may take over.
func (m *Machine) fenceIfCutOff(why string) {
	m.mu.Lock()
	if m.cutOffSince.IsZero() {
		m.cutOffSince = time.Now()
		m.log.Printf("%s", why)
	}
	tooLong := time.Since(m.cutOffSince) >= m.cfg.FailoverAfter()/2
	leading := m.leading
	m.mu.Unlock()
	if leading && tooLong {
		m.releaseLocally(why + " for too long; another machine may take over")
	}
}

func (m *Machine) connected() {
	m.mu.Lock()
	m.cutOffSince = time.Time{}
	m.mu.Unlock()
}

// handOff moves the accounts the planned way: this machine stops its proxy, sends its final
// logins, and the target starts only once it has them. When the outcome is unknown, this machine
// stays stopped: a second proxy is worse than a pause, and the next look settles who leads.
func (m *Machine) handOff(ctx context.Context, target string) error {
	m.transition.Lock()
	defer m.transition.Unlock()
	m.mu.Lock()
	leading, epoch := m.leading, m.state.Epoch
	_, answers := m.peers.answered[target]
	m.mu.Unlock()
	switch {
	case !leading:
		return errors.New("this machine doesn't hold the accounts")
	case target == m.selfName():
		return nil
	case !answers:
		return fmt.Errorf("%s doesn't answer, or can't hold the accounts", target)
	}
	m.mu.Lock()
	m.leading = false
	m.mu.Unlock()
	m.o.Proxy.Stop(m.quiet)
	err := m.sendAccounts(ctx, target, epoch+1)
	switch {
	case err == nil || m.leadsNow(target):
		m.handedOff(target, epoch+1)
		return nil
	case api.WasRefused(err):
		m.log.Printf("%s refused the accounts (%v); resuming", target, err)
		if resumeErr := m.startLeading(ctx, epoch); resumeErr != nil {
			return fmt.Errorf("%w; and couldn't resume: %v", err, resumeErr)
		}
		return err
	}
	m.log.Printf("hand-off to %s: no answer (%v); staying stopped until it is clear who leads", target, err)
	return err
}

func (m *Machine) leadsNow(target string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var l api.Leader
	return m.getPeer(ctx, target, api.PathLeader, &l) == nil && l.Leading
}

func (m *Machine) handedOff(target string, epoch int64) {
	m.mu.Lock()
	m.state.Leader, m.state.Epoch = target, epoch
	m.leaderAddr = ""
	m.replica.pulled = time.Time{}
	m.mu.Unlock()
	m.save()
	m.log.Printf("handed the accounts to %s (epoch %d)", target, epoch)
}

func (m *Machine) sendAccounts(ctx context.Context, target string, epoch int64) error {
	files, err := logins.Read(m.cfg.AuthDir)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	accounts := api.Logins{From: m.selfName(), Epoch: epoch, Complete: true, Files: files}
	return m.callPeer(ctx, target, api.PathReceive, accounts, nil)
}

// receive takes the accounts from the leader this machine follows. It runs to the end even if
// the sender stops waiting, so the sender can ask afterwards who leads.
func (m *Machine) receive(accounts api.Logins) error {
	m.transition.Lock()
	defer m.transition.Unlock()
	m.mu.Lock()
	leading, epoch, leader := m.leading, m.state.Epoch, m.state.Leader
	m.mu.Unlock()
	switch {
	case leading:
		return errors.New("already holding the accounts")
	case accounts.From != leader:
		return fmt.Errorf("%s isn't the leader this machine follows (%s)", accounts.From, leader)
	case accounts.Epoch <= epoch:
		return fmt.Errorf("stale hand-off (epoch %d, this machine is at %d)", accounts.Epoch, epoch)
	}
	if _, err := logins.Merge(m.cfg.AuthDir, m.cfg.RemovedDir(), accounts.Files, logins.Everything); err != nil {
		return err
	}
	m.log.Printf("taking over the accounts from %s", accounts.From)
	return m.startLeading(context.Background(), accounts.Epoch)
}

// shutdown hands the accounts to a synced machine if there is one, so a planned stop never
// leaves them unheld.
func (m *Machine) shutdown() {
	if m.isLeading() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		target := m.handOffTarget(ctx)
		if target == "" || m.handOff(ctx, target) != nil {
			m.releaseLocally("service stopping and no synced machine to hand the accounts to")
		}
		cancel()
	}
	m.mu.Lock()
	m.state.LastAlive = time.Now().Round(0)
	m.mu.Unlock()
	m.save()
}

// handOffTarget asks the members afresh, since the last look may be a tick old, and prefers the hub.
func (m *Machine) handOffTarget(ctx context.Context) string {
	m.mu.Lock()
	ask := map[string]tailnet.Node{}
	for name := range maps.Keys(m.state.Members) {
		if node, ok := m.tailnet.Peer(name); ok && node.Online {
			ask[name] = node
		}
	}
	epoch := m.state.Epoch
	m.mu.Unlock()

	best, bestIsHub := "", false
	for name, r := range m.probe(ctx, ask) {
		if peerState(r, epoch) != leadership.Synced {
			continue
		}
		isHub := r.Hold == config.HoldHub
		if best == "" || (isHub && !bestIsHub) || (isHub == bestIsHub && name < best) {
			best, bestIsHub = name, isHub
			m.mu.Lock()
			m.peers.answered[name] = r
			m.mu.Unlock()
		}
	}
	return best
}

// quiet reports whether no login changed during the last second, so no refresh is mid-write.
func (m *Machine) quiet() bool {
	before := logins.Fingerprint(m.cfg.AuthDir)
	time.Sleep(time.Second)
	return before == logins.Fingerprint(m.cfg.AuthDir)
}
