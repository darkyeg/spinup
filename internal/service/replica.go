package service

import (
	"sync"
	"time"

	"github.com/darkyeg/spinup/internal/api"
)

const pullEvery = 30 * time.Second

// replica tracks this machine's copy of the leader's logins, or, on the leader, what it pushed.
type replica struct {
	mu            sync.Mutex
	epoch         int64
	at            time.Time
	pulled        pace
	pushed        string
	offeredLeader string
	offeredEpoch  int64
}

func (r *replica) offered(leader string, epoch int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.offeredLeader, r.offeredEpoch = leader, epoch
}

func (r *replica) offeredTo(leader string, epoch int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.offeredLeader == leader && r.offeredEpoch == epoch
}

// pullDue reports whether a complete pull is due, and counts it as started.
func (r *replica) pullDue() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pulled.due(pullEvery)
}

func (r *replica) pullSoon() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pulled.last = time.Time{}
}

// merged records a pull; a complete one makes this machine a synced copy of epoch.
func (r *replica) merged(epoch int64, complete bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pulled.last = time.Now()
	if complete {
		r.epoch, r.at = epoch, time.Now()
	}
}

// pushedAlready reports whether fingerprint is what the leader pushed last, and remembers it.
func (r *replica) pushedAlready(fingerprint string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	same := fingerprint == r.pushed
	r.pushed = fingerprint
	return same
}

func (r *replica) pushAgain() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pushed = ""
}

func (r *replica) synced(now time.Time) *api.Sync {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.at.IsZero() {
		return nil
	}
	return &api.Sync{Epoch: r.epoch, SecondsAgo: int(now.Sub(r.at).Seconds())}
}
