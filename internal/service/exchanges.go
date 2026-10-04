package service

import (
	"sync"
	"time"
)

// exchanges keeps library transfers from repeating: a partner that can't keep what it gets (an older
// spinup that deletes fetched copies, say) would otherwise receive the whole library again every round.
type exchanges struct {
	mu sync.Mutex
	// last is, per partner, the situation the last transfer with it started from.
	last map[string]string
	// served is when each machine last got the whole library from this one.
	served map[string]time.Time
}

// repeats reports whether the last transfer with partner started from situation, so another would change nothing.
func (e *exchanges) repeats(partner, situation string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.last[partner] == situation
}

// record remembers the situation a transfer with partner started from; when nothing was sent it forgets
// it, so a partner that later loses something gets it again.
func (e *exchanges) record(partner, situation string, sent bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !sent {
		delete(e.last, partner)
		return
	}
	if e.last == nil {
		e.last = map[string]string{}
	}
	e.last[partner] = situation
}

// mayServe reports whether caller may get the whole library now, and counts it as served.
func (e *exchanges) mayServe(caller string, now time.Time, gap time.Duration) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if last, ok := e.served[caller]; ok && now.Sub(last) < gap {
		return false
	}
	if e.served == nil {
		e.served = map[string]time.Time{}
	}
	e.served[caller] = now
	return true
}
