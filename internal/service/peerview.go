package service

import (
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/leadership"
	"github.com/darkyeg/spinup/internal/tailnet"
)

const (
	// syncedWithin: a copy older than this doesn't count as synced.
	syncedWithin  = 2 * time.Minute
	discoverEvery = 10 * time.Second
)

// peerView is what this machine saw of the others at the last look.
type peerView struct {
	mu           sync.Mutex
	list         []leadership.Peer
	answered     map[string]api.Report
	offlineSince map[string]time.Time
	discover     pace
}

func newPeerView() *peerView {
	return &peerView{answered: map[string]api.Report{}, offlineSince: map[string]time.Time{}}
}

func (v *peerView) discoverDue() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.discover.due(discoverEvery)
}

// see replaces the view with a new look and returns the peers as leadership.Decide takes them.
func (v *peerView) see(in sighting) []leadership.Peer {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.answered = in.answered
	v.list = describePeers(in, v.offlineSince)
	return slices.Clone(v.list)
}

func (v *peerView) peers() []leadership.Peer {
	v.mu.Lock()
	defer v.mu.Unlock()
	return slices.Clone(v.list)
}

func (v *peerView) reportOf(name string) (api.Report, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	r, ok := v.answered[name]
	return r, ok
}

func (v *peerView) answeredNow(name string, r api.Report) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.answered[name] = r
}

// holders are the machines that can hold and answered the last look.
func (v *peerView) holders() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	var names []string
	for name, r := range v.answered {
		if r.Hold.CanHold() {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// sighting is everything one look at the tailnet found out.
type sighting struct {
	members   map[string]member
	tailnet   tailnet.Status
	answered  map[string]api.Report
	epoch     int64
	bootAlive time.Time
	failover  time.Duration
	now       time.Time
}

// describePeers turns a look into leadership's peers; offlineSince remembers each offline machine's first sight.
func describePeers(in sighting, offlineSince map[string]time.Time) []leadership.Peer {
	var peers []leadership.Peer
	for name, mem := range in.members {
		if name == in.tailnet.Self.Name {
			continue
		}
		p := leadership.Peer{Name: name, Hold: mem.Hold, Epoch: mem.Epoch}
		node, inTailnet := in.tailnet.Peer(name)
		switch r, ok := in.answered[name]; {
		case ok:
			p.State, p.Epoch = peerState(r, in.epoch), r.Epoch
		case inTailnet && node.Online:
			p.State = leadership.Silent
		default:
			p.State = leadership.Offline
		}
		if p.State == leadership.Offline {
			first, seen := offlineSince[name]
			if !seen {
				first = in.now
				offlineSince[name] = first
			}
			p.OfflineFor, p.MayHaveLed = offlineFacts(first, node, inTailnet, in)
		} else {
			delete(offlineSince, name)
		}
		peers = append(peers, p)
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].Name < peers[j].Name })
	return peers
}

// offlineFacts counts from Tailscale's last sight, which may predate this service's start.
func offlineFacts(firstSeen time.Time, node tailnet.Node, inTailnet bool, in sighting) (offlineFor time.Duration, mayHaveLed bool) {
	since := firstSeen
	if inTailnet && !node.LastSeen.IsZero() && node.LastSeen.Before(since) {
		since = node.LastSeen
	}
	mayHaveLed = !in.bootAlive.IsZero() && inTailnet && node.LastSeen.After(in.bootAlive.Add(in.failover))
	return in.now.Sub(since), mayHaveLed
}

func peerState(r api.Report, epoch int64) leadership.PeerState {
	switch {
	case r.Leading:
		return leadership.Leading
	case r.Starting:
		return leadership.Starting
	case r.Synced != nil && r.Synced.Epoch == epoch && r.Synced.SecondsAgo >= 0 &&
		time.Duration(r.Synced.SecondsAgo)*time.Second < syncedWithin:
		return leadership.Synced
	}
	return leadership.Standing
}

func asMembers(reports map[string]api.Report) map[string]member {
	members := map[string]member{}
	for name, r := range reports {
		if r.Hold.CanHold() {
			members[name] = member{Hold: r.Hold, Epoch: r.Epoch}
		}
	}
	return members
}
