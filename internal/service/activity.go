package service

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/darkyeg/spinup/internal/leadership"
)

const neverBusy = time.Duration(math.MaxInt64)

var errMustStop = errors.New("this machine must stop using the accounts")

// activity counts the requests this machine serves through its own proxy and when the last one ended.
type activity struct {
	mu       sync.Mutex
	running  int
	lastDone time.Time
	// ended is closed, and replaced, whenever a request ends or the machine must stop now.
	ended chan struct{}
	// closed turns new requests away once a drain ended, until the proxy serves again.
	closed bool
	// urgent ends drains at once, until the proxy serves again.
	urgent   bool
	stopping chan struct{}
}

// begin counts a request, unless a drain closed the proxy to new ones; end ends it and is safe to call more than once.
func (a *activity) begin() (end func(), ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil, false
	}
	a.running++
	var once sync.Once
	return func() { once.Do(a.finish) }, true
}

// open lets requests in again and forgets an urgent stop: the proxy serves again.
func (a *activity) open() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed, a.urgent = false, false
	a.stopping = nil
}

func (a *activity) resume() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.urgent {
		return false
	}
	a.closed = false
	return true
}

func (a *activity) stopRequested() <-chan struct{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopping == nil {
		a.stopping = make(chan struct{})
		if a.urgent {
			close(a.stopping)
		}
	}
	return a.stopping
}

// hurry ends any drain now: the machine must stop using the accounts at once.
func (a *activity) hurry() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	if !a.urgent {
		a.urgent = true
		if a.stopping != nil {
			close(a.stopping)
		}
	}
	a.wake()
}

// mustStop reports whether hurry was called since the proxy last served.
func (a *activity) mustStop() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.urgent
}

func (a *activity) finish() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.running--
	a.lastDone = time.Now()
	a.wake()
}

// wake needs a.mu held.
func (a *activity) wake() {
	if a.ended != nil {
		close(a.ended)
		a.ended = nil
	}
}

func (a *activity) snapshot(now time.Time) leadership.Activity {
	a.mu.Lock()
	defer a.mu.Unlock()
	idle := neverBusy
	if !a.lastDone.IsZero() {
		idle = now.Sub(a.lastDone)
	}
	return leadership.Activity{InFlight: a.running, IdleFor: idle}
}

func (a *activity) inFlight() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running
}

// pause closes admission only after requests finish naturally; cancellation leaves them serving.
func (a *activity) pause(ctx context.Context) error {
	for {
		a.mu.Lock()
		switch {
		case a.urgent:
			a.mu.Unlock()
			return errMustStop
		case ctx.Err() != nil:
			a.mu.Unlock()
			return ctx.Err()
		case a.running == 0:
			a.closed = true
			a.mu.Unlock()
			return nil
		}
		if a.ended == nil {
			a.ended = make(chan struct{})
		}
		ended := a.ended
		a.mu.Unlock()
		select {
		case <-ended:
		case <-ctx.Done():
		}
	}
}

// drain waits until no request runs, for at most limit and never after hurry, then turns new requests away
// and returns how many still run. New requests keep being served while it waits.
func (a *activity) drain(ctx context.Context, limit time.Duration) (left int) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	for {
		a.mu.Lock()
		if a.running == 0 || a.urgent || ctx.Err() != nil {
			a.closed = true
			left = a.running
			a.mu.Unlock()
			return left
		}
		if a.ended == nil {
			a.ended = make(chan struct{})
		}
		ended := a.ended
		a.mu.Unlock()
		select {
		case <-ended:
		case <-ctx.Done():
		}
	}
}
