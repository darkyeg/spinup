package service

import (
	"sync"
	"time"
)

// fence times how long this machine has been cut off from Tailscale.
type fence struct {
	mu    sync.Mutex
	since time.Time
}

// cutOff reports how long the cut has lasted and whether this call started it.
func (f *fence) cutOff(now time.Time) (elapsed time.Duration, started bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.since.IsZero() {
		f.since, started = now, true
	}
	return now.Sub(f.since), started
}

func (f *fence) connected() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.since = time.Time{}
}

// fenceAfter is how long a leader that noticed it lost Tailscale keeps going, to ride out a blip.
const fenceAfter = 10 * time.Second

// cutOffTooLong: a leader stops fenceAfter after it notices the cut (sooner when failoverAfter is short), which
// the shortest failover time config allows keeps well before any other machine may take over.
func cutOffTooLong(elapsed, failoverAfter time.Duration) bool {
	return elapsed >= min(fenceAfter, failoverAfter/2)
}
