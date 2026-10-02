package service

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/logins"
	"github.com/darkyeg/spinup/internal/tailnet"
)

// syncTimeout bounds one login transfer, so an unreachable machine can't stall a tick.
const syncTimeout = 10 * time.Second

// follow sends a new leader this machine's logins once, then pulls its complete set now and then.
func (m *Machine) follow(ctx context.Context, leader string, epoch int64, ts tailnet.Status) {
	addr := ""
	if node, ok := ts.Peer(leader); ok {
		addr = m.o.PeerAddr(leader, node.IP)
	}
	changed := m.ledger.follow(leader, epoch)
	if changed {
		m.replica.pullSoon()
	}
	m.notes.routeTo(addr)
	pull := m.replica.pullDue()
	if changed {
		m.log.Printf("following %s (epoch %d)", leader, epoch)
		m.save()
		if err := m.pushLogins(ctx, leader, logins.Some); err != nil {
			m.log.Printf("send logins to %s: %v", leader, err)
		}
	}
	if pull && addr != "" {
		m.pullFromLeader(ctx, leader)
	}
}

func (m *Machine) pullFromLeader(ctx context.Context, leader string) {
	incoming, err := m.pullLogins(ctx, leader)
	if err != nil {
		m.log.Printf("sync from %s: %v", leader, err)
		return
	}
	m.mergeFromLeader(incoming)
}

func (m *Machine) mergeFromLeader(incoming api.Logins) {
	merged, err := logins.Merge(m.cfg.AuthDir, m.cfg.RemovedDir(), incoming.Files, extentOf(incoming))
	if err != nil {
		m.log.Printf("sync from %s: %v", incoming.From, err)
		return
	}
	if merged.Changed() {
		m.log.Printf("synced from %s: updated %v, removed %v", incoming.From, merged.Written, merged.Removed)
	}
	m.replica.merged(incoming.Epoch, incoming.Complete)
}

// fromLeader reports whether incoming comes from the machine this one follows.
func (m *Machine) fromLeader(incoming api.Logins) bool {
	st := m.ledger.view(time.Now())
	return !st.claiming() && incoming.From == st.Leader && incoming.Epoch >= st.Epoch
}

// pushIfChanged sends the logins to every machine that can hold as soon as the proxy refreshes one.
func (m *Machine) pushIfChanged(ctx context.Context) {
	fingerprint := logins.Fingerprint(m.cfg.AuthDir)
	if m.replica.pushedAlready(fingerprint) {
		return
	}
	eachPeer(m.peers.holders(), func(peer string) {
		if err := m.pushLogins(ctx, peer, logins.Everything); err != nil {
			m.log.Printf("push logins to %s: %v", peer, err)
		}
	})
}

// takeNewerLogins merges in every login another machine refreshed later than this one did.
func (m *Machine) takeNewerLogins(ctx context.Context) []string {
	var mu sync.Mutex
	var written []string
	eachPeer(m.peers.holders(), func(peer string) {
		incoming, err := m.pullLogins(ctx, peer)
		if err != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if merged, err := logins.Merge(m.cfg.AuthDir, m.cfg.RemovedDir(), incoming.Files, logins.Some); err == nil {
			written = append(written, merged.Written...)
		}
	})
	sort.Strings(written)
	return written
}

func (m *Machine) pullLogins(ctx context.Context, peer string) (api.Logins, error) {
	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	var incoming api.Logins
	err := m.getPeer(ctx, peer, api.PathLogins, &incoming)
	return incoming, err
}

func (m *Machine) pushLogins(ctx context.Context, peer string, extent logins.Extent) error {
	outgoing, err := m.ownLogins(extent)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	return m.callPeer(ctx, peer, api.PathLogins, outgoing, nil)
}

func (m *Machine) ownLogins(extent logins.Extent) (api.Logins, error) {
	files, err := logins.Read(m.cfg.AuthDir)
	return api.Logins{From: m.tail.selfName(), Epoch: m.ledger.view(time.Now()).Epoch, Complete: extent == logins.Everything, Files: files}, err
}

func extentOf(l api.Logins) logins.Extent {
	if l.Complete {
		return logins.Everything
	}
	return logins.Some
}
