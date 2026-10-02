package service

import (
	"context"
	"time"
)

// watchSleep stops a leader that slept, since it may have been replaced, until it has checked again.
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
			m.ledger.wokeAt(last)
			m.log.Printf("woke up after %s", asleep.Round(time.Second))
			m.releaseLocally("woke from sleep; checking that nobody took over")
		}
		last = now
	}
}
