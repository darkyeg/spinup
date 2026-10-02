package service

import "time"

// pace limits how often something runs; the lock of whatever holds it guards it.
type pace struct{ last time.Time }

func (p *pace) due(every time.Duration) bool {
	if time.Since(p.last) < every {
		return false
	}
	p.last = time.Now()
	return true
}
