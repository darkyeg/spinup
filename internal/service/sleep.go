package service

import (
	"context"
	"time"
)

// watchSleep: a leader that slept may have been replaced meanwhile, so on waking it stops using
// the accounts at once and re-checks before using them again.
func (m *Machine) watchSleep(ctx context.Context) {
	last := time.Now().Round(0) // the wall clock; the monotonic one may not count sleep
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
		now := time.Now().Round(0)
		asleep := now.Sub(last)
		if asleep >= 15*time.Second {
			m.mu.Lock()
			m.bootAlive = last // the last moment known awake; a tick may already have moved LastAlive past the sleep
			m.mu.Unlock()
			m.log.Printf("woke up after %s", asleep.Round(time.Second))
			m.releaseLocally("woke from sleep; checking that nobody took over")
		}
		last = now
	}
}
