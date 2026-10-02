package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/leadership"
	"github.com/darkyeg/spinup/internal/logins"
	"github.com/darkyeg/spinup/internal/tailnet"
)

const (
	proxyStartTimeout  = 45 * time.Second
	sendAccountsWithin = 60 * time.Second
	shutdownWithin     = 20 * time.Second
)

// lead takes newer logins from the other machines first, then starts if nobody else holds the accounts.
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

// stillUnheld needs m.transition held.
func (m *Machine) stillUnheld(ctx context.Context, epoch int64) error {
	switch st := m.ledger.view(time.Now()); {
	case st.claiming():
		return errAlreadyHolding
	case st.Epoch >= epoch:
		return fmt.Errorf("epoch %d was overtaken by %d", epoch, st.Epoch)
	}
	for _, peer := range m.peers.holders() {
		if m.claimedBy(ctx, peer) {
			return fmt.Errorf("%s holds the accounts now", peer)
		}
	}
	return nil
}

func (m *Machine) claimedBy(ctx context.Context, peer string) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var l api.Leader
	return m.askPeer(ctx, peer, api.PathLeader, &l) == nil && l.Name == peer && (l.Leading || l.Starting)
}

// startLeading claims the accounts before the proxy starts, so peers see the claim; needs m.transition held.
func (m *Machine) startLeading(ctx context.Context, epoch int64) error {
	previous := m.ledger.beginLeading(epoch, m.tail.selfName())
	m.save()
	return m.startProxy(ctx, epoch, previous)
}

func (m *Machine) startProxy(ctx context.Context, epoch int64, previous claim) error {
	if err := m.launchProxy(ctx); err != nil {
		m.ledger.abandonLeading(previous)
		m.save()
		return err
	}
	m.ledger.finishLeading()
	m.notes.routeTo("")
	m.replica.pushAgain()
	m.save()
	m.log.Printf("this machine now holds the accounts (epoch %d)", epoch)
	return nil
}

func (m *Machine) launchProxy(ctx context.Context) error {
	if m.o.PrepareProxy != nil {
		if err := m.o.PrepareProxy(); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, proxyStartTimeout)
	defer cancel()
	if err := m.o.Proxy.Start(ctx); err != nil {
		m.o.Proxy.Stop(nil)
		return err
	}
	return nil
}

// stepDown stops at once: another machine runs the accounts, so waiting for quiet would only let both refresh.
func (m *Machine) stepDown(ctx context.Context, leader string, epoch int64, ts tailnet.Status) {
	m.transition.Lock()
	if m.ledger.stopLeading() {
		m.o.Proxy.Stop(nil)
		m.log.Printf("stopped using the accounts: %s holds them (epoch %d)", leader, epoch)
	}
	m.transition.Unlock()
	m.follow(ctx, leader, epoch, ts)
}

// releaseLocally stops without handing on; the machine stays the known leader and resumes if nobody took over.
func (m *Machine) releaseLocally(why string) {
	m.transition.Lock()
	defer m.transition.Unlock()
	if m.ledger.stopLeading() {
		m.o.Proxy.Stop(m.quiet)
		m.log.Printf("stopped using the accounts: %s", why)
	}
}

func (m *Machine) fenceIfCutOff(why string) {
	elapsed, started := m.fence.cutOff(time.Now())
	if started {
		m.log.Printf("%s", why)
	}
	if m.ledger.view(time.Now()).Leading && cutOffTooLong(elapsed, m.cfg.FailoverAfter()) {
		m.releaseLocally(why + " for too long; another machine may take over")
	}
}

// handOff stops the proxy and sends the final logins; if the target's answer is unknown it presumes the target holds them.
func (m *Machine) handOff(ctx context.Context, target string) error {
	m.transition.Lock()
	defer m.transition.Unlock()
	st := m.ledger.view(time.Now())
	_, answers := m.peers.reportOf(target)
	switch {
	case !st.Leading:
		return errors.New("this machine doesn't hold the accounts")
	case target == m.tail.selfName():
		return nil
	case !answers:
		return fmt.Errorf("%s doesn't answer, or can't hold the accounts", target)
	}
	m.ledger.stopLeading()
	m.o.Proxy.Stop(m.quiet)
	err := m.sendAccounts(ctx, target, st.Epoch+1)
	switch {
	case err == nil || m.claimedBy(context.Background(), target):
		m.handedOff(target, st.Epoch+1)
		return nil
	case api.WasRefused(err):
		m.log.Printf("%s refused the accounts (%v); resuming", target, err)
		if resumeErr := m.startLeading(ctx, st.Epoch); resumeErr != nil {
			return fmt.Errorf("%w; and couldn't resume: %v", err, resumeErr)
		}
		return err
	}
	m.ledger.handedOffUnknown(target, st.Epoch+1, time.Now())
	m.notes.routeTo("")
	m.save()
	m.log.Printf("hand-off to %s: no answer (%v); presuming it holds the accounts until it shows otherwise", target, err)
	return err
}

func (m *Machine) handedOff(target string, epoch int64) {
	m.ledger.handedOff(target, epoch)
	m.notes.routeTo("")
	m.replica.pullSoon()
	m.save()
	m.log.Printf("handed the accounts to %s (epoch %d)", target, epoch)
}

func (m *Machine) sendAccounts(ctx context.Context, target string, epoch int64) error {
	files, err := logins.Read(m.cfg.AuthDir)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, sendAccountsWithin)
	defer cancel()
	accounts := api.Logins{From: m.tail.selfName(), Epoch: epoch, Complete: true, Files: files}
	return m.callPeer(ctx, target, api.PathReceive, accounts, nil)
}

// receive claims the accounts before it merges and starts, so a sender that timed out can see the claim.
func (m *Machine) receive(accounts api.Logins) error {
	m.transition.Lock()
	defer m.transition.Unlock()
	switch st := m.ledger.view(time.Now()); {
	case st.claiming():
		return errAlreadyHolding
	case accounts.From != st.Leader:
		return fmt.Errorf("%s isn't the leader this machine follows (%s)", accounts.From, st.Leader)
	case accounts.Epoch <= st.Epoch:
		return fmt.Errorf("stale hand-off (epoch %d, this machine is at %d)", accounts.Epoch, st.Epoch)
	}
	previous := m.ledger.beginLeading(accounts.Epoch, m.tail.selfName())
	m.save()
	if _, err := logins.Merge(m.cfg.AuthDir, m.cfg.RemovedDir(), accounts.Files, logins.Everything); err != nil {
		m.ledger.abandonLeading(previous)
		m.save()
		return err
	}
	m.log.Printf("taking over the accounts from %s", accounts.From)
	return m.startProxy(context.Background(), accounts.Epoch, previous)
}

// shutdown hands the accounts to a synced machine if there is one, so a planned stop never leaves them unheld.
func (m *Machine) shutdown() {
	if m.ledger.view(time.Now()).Leading {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownWithin)
		target := m.handOffTarget(ctx)
		if target == "" || m.handOff(ctx, target) != nil {
			m.releaseLocally("service stopping and no synced machine to hand the accounts to")
		}
		cancel()
	}
	m.ledger.touch(time.Now())
	m.save()
}

// handOffTarget asks the members afresh, since the last look may be a tick old, and prefers the hub.
func (m *Machine) handOffTarget(ctx context.Context) string {
	epoch := m.ledger.view(time.Now()).Epoch
	ask := map[string]tailnet.Node{}
	for name := range m.ledger.members() {
		if node, ok := m.tail.peer(name); ok && node.Online {
			ask[name] = node
		}
	}
	reports := m.probe(ctx, ask)
	target := syncedTarget(reports, epoch)
	if r, ok := reports[target]; ok {
		m.peers.answeredNow(target, r)
	}
	return target
}

// syncedTarget prefers the hub among the machines holding a current copy, then the first name.
func syncedTarget(reports map[string]api.Report, epoch int64) string {
	best, bestIsHub := "", false
	for name, r := range reports {
		if peerState(r, epoch) != leadership.Synced {
			continue
		}
		isHub := r.Hold == config.HoldHub
		if best == "" || (isHub && !bestIsHub) || (isHub == bestIsHub && name < best) {
			best, bestIsHub = name, isHub
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
