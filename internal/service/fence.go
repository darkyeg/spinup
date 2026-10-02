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

// cutOffTooLong: a leader cut off from Tailscale stops at half of failoverAfter, before another machine may take over.
func cutOffTooLong(elapsed, failoverAfter time.Duration) bool { return elapsed >= failoverAfter/2 }
