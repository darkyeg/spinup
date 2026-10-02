package service

import "sync"

// outlook is what this machine tells people: why it waits and where requests go.
type outlook struct {
	mu         sync.Mutex
	waiting    string
	leaderAddr string
}

// note records why the machine waits, empty when it doesn't, and reports a new reason.
func (o *outlook) note(reason string) (isNew bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	isNew = reason != "" && reason != o.waiting
	o.waiting = reason
	if reason != "" {
		o.leaderAddr = ""
	}
	return isNew
}

func (o *outlook) routeTo(addr string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.leaderAddr = addr
}

func (o *outlook) get() (waiting, leaderAddr string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.waiting, o.leaderAddr
}
