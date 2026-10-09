package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/logins"
	"github.com/darkyeg/spinup/internal/tailnet"
)

// syncTimeout bounds one login transfer, so an unreachable machine can't stall a tick.
const syncTimeout = 10 * time.Second

// follow retries the initial offer until accepted, then pulls the leader's complete set.
func (m *Machine) follow(ctx context.Context, leader string, epoch int64, ts tailnet.Status) {
	addr := ""
	if node, ok := ts.Peer(leader); ok {
		addr = m.o.PeerAddr(leader, node.IP)
	}
	m.transition.Lock()
	st := m.ledger.view(time.Now())
	if ctx.Err() != nil || st.claiming() || epoch < st.Epoch {
		m.transition.Unlock()
		return
	}
	changed := m.ledger.follow(leader, epoch)
	m.notes.routeTo(addr)
	m.transition.Unlock()
	if changed {
		m.replica.pullSoon()
	}
	pull := m.replica.pullDue()
	if changed {
		m.log.Printf("following %s (epoch %d)", leader, epoch)
		m.save()
	}
	if pull && addr != "" {
		if !m.replica.offeredTo(leader, epoch) {
			if err := m.pushLogins(ctx, leader, logins.Some); err != nil {
				m.log.Printf("send logins to %s: %v", leader, err)
				m.replica.pullSoon()
				return
			}
			m.replica.offered(leader, epoch)
		}
		m.pullFromLeader(ctx, leader)
	}
}

func (m *Machine) pullFromLeader(ctx context.Context, leader string) {
	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	url, err := m.provenURL(leader, api.PathLogins)
	if err != nil {
		m.log.Printf("sync from %s: %v", leader, err)
		return
	}
	client := m.client()
	st := m.ledger.view(time.Now())
	if m.replica.offeredTo(leader, st.Epoch) {
		client.IfNoneMatch = m.loginTag(leader, st.Epoch)
	}
	var incoming api.Logins
	err = client.Get(ctx, url, &incoming)
	if errors.Is(err, api.ErrNotModified) {
		m.confirmLoginCopy(leader, st.Epoch, client.IfNoneMatch)
		return
	}
	if err != nil {
		m.log.Printf("sync from %s: %v", leader, err)
		return
	}
	if _, err := m.mergeLogins(ctx, incoming); err != nil {
		m.log.Printf("sync from %s: %v", incoming.From, err)
	}
}

func (m *Machine) loginTag(leader string, epoch int64) string {
	files, err := logins.Read(m.cfg.AuthDir)
	if err != nil {
		return ""
	}
	return (api.Logins{From: leader, Epoch: epoch, Complete: true, Files: files}).ETag()
}

func (m *Machine) confirmLoginCopy(leader string, epoch int64, tag string) {
	m.transition.Lock()
	defer m.transition.Unlock()
	st := m.ledger.view(time.Now())
	if st.claiming() || st.Leader != leader || st.Epoch != epoch || m.loginTag(leader, epoch) != tag {
		m.replica.pullSoon()
		return
	}
	m.replica.merged(epoch, true)
}

// fromLeader reports whether incoming comes from the machine this one follows.
func (m *Machine) fromLeader(incoming api.Logins) bool {
	st := m.ledger.view(time.Now())
	return !st.claiming() && st.Leader != "" && incoming.From == st.Leader && incoming.Epoch >= st.Epoch
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
			m.replica.pushAgain()
		}
	})
}

func (m *Machine) collectLogins(ctx context.Context) []logins.File {
	var mu sync.Mutex
	var files []logins.File
	eachPeer(m.peers.holders(), func(peer string) {
		incoming, err := m.pullLogins(ctx, peer)
		if err != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		files = append(files, incoming.Files...)
	})
	return files
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
