package service

import (
	"sync"

	"github.com/darkyeg/spinup/internal/tailnet"
)

// tailView is the tailnet as the last tick saw it.
type tailView struct {
	mu     sync.Mutex
	status tailnet.Status
}

func (t *tailView) set(s tailnet.Status) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.status = s
}

func (t *tailView) self() tailnet.Node {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status.Self
}

func (t *tailView) selfName() string { return t.self().Name }

func (t *tailView) peer(name string) (tailnet.Node, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status.Peer(name)
}
