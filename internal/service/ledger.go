package service

import (
	"encoding/json"
	"errors"
	"maps"
	"sync"
	"time"

	"github.com/darkyeg/spinup/internal/atomicfile"
	"github.com/darkyeg/spinup/internal/config"
)

// forcedFor is how long a `spinup takeover` stays valid when this machine never starts leading.
const forcedFor = time.Minute

var errAlreadyHolding = errors.New("this machine already holds the accounts")

// ledger is who holds the accounts as far as this machine knows, under one lock so a claim is never seen half made.
type ledger struct {
	mu        sync.Mutex
	writeMu   sync.Mutex
	path      string
	st        persisted
	bootAlive time.Time
	leading   bool
	starting  bool
	forcedAt  time.Time
	saved     pace
}

// standing is a consistent look at the ledger.
type standing struct {
	Leading, Starting, Forced bool
	Epoch                     int64
	Leader                    string
	HandOff                   *unconfirmedHandOff
	BootAlive                 time.Time
}

// claim is the epoch and leader a start would replace.
type claim struct {
	epoch  int64
	leader string
}

func (s standing) claiming() bool { return s.Leading || s.Starting }

func newLedger(path string) *ledger {
	return &ledger{path: path, st: persisted{Members: map[string]member{}}}
}

func (l *ledger) load() error {
	st, err := loadState(l.path)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.st, l.bootAlive = st, st.LastAlive
	return nil
}

func (l *ledger) view(now time.Time) standing {
	l.mu.Lock()
	defer l.mu.Unlock()
	return standing{
		Leading: l.leading, Starting: l.starting, Epoch: l.st.Epoch, Leader: l.st.Leader,
		Forced: !l.forcedAt.IsZero() && now.Sub(l.forcedAt) < forcedFor, HandOff: l.st.HandOff, BootAlive: l.bootAlive,
	}
}

func (l *ledger) save() error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	l.mu.Lock()
	st := l.st
	st.Members = maps.Clone(l.st.Members)
	l.saved.last = time.Now()
	l.mu.Unlock()
	data, _ := json.MarshalIndent(st, "", "  ")
	return atomicfile.Write(l.path, append(data, '\n'), 0o600)
}

// touch records the wall-clock time and reports whether it is time to write it to disk.
func (l *ledger) touch(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.st.LastAlive = now.Round(0)
	return l.saved.due(15 * time.Second)
}

func (l *ledger) wokeAt(last time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.bootAlive = last
}

func (l *ledger) takeover(now time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.leading || l.starting {
		return errAlreadyHolding
	}
	l.forcedAt = now
	return nil
}

// beginLeading claims the accounts for self before the proxy starts, so that peers see the claim.
func (l *ledger) beginLeading(epoch int64, self string) claim {
	l.mu.Lock()
	defer l.mu.Unlock()
	previous := claim{l.st.Epoch, l.st.Leader}
	l.st.Epoch, l.st.Leader, l.st.HandOff = epoch, self, nil
	l.starting, l.forcedAt = true, time.Time{}
	return previous
}

func (l *ledger) finishLeading() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.leading, l.starting = true, false
}

// abandonLeading undoes a claim whose proxy didn't start.
func (l *ledger) abandonLeading(previous claim) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.starting {
		l.st.Epoch, l.st.Leader, l.starting = previous.epoch, previous.leader, false
	}
}

// stopLeading reports whether the machine was leading. It stays the known leader.
func (l *ledger) stopLeading() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	was := l.leading
	l.leading, l.forcedAt = false, time.Time{}
	return was
}

// follow records the leader another machine claims and reports whether that changed anything.
func (l *ledger) follow(leader string, epoch int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.forcedAt = time.Time{}
	changed := l.st.Leader != leader || epoch > l.st.Epoch
	if changed {
		l.st.Leader, l.st.Epoch = leader, max(l.st.Epoch, epoch)
	}
	if l.st.HandOff != nil && l.st.HandOff.Target != leader {
		l.st.HandOff = nil
	}
	return changed
}

func (l *ledger) handedOff(target string, epoch int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.st.Leader, l.st.Epoch, l.st.HandOff = target, epoch, nil
}

// handedOffUnknown presumes target holds the accounts at epoch until it shows otherwise.
func (l *ledger) handedOffUnknown(target string, epoch int64, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.st.Leader, l.st.Epoch, l.st.HandOff = target, epoch, &unconfirmedHandOff{Target: target, At: now.Round(0)}
}

func (l *ledger) members() map[string]member {
	l.mu.Lock()
	defer l.mu.Unlock()
	return maps.Clone(l.st.Members)
}

func (l *ledger) recordMembers(seen map[string]member) {
	l.mu.Lock()
	defer l.mu.Unlock()
	maps.Copy(l.st.Members, seen)
}

// meet adds a machine that can hold and reports whether it was new.
func (l *ledger) meet(name string, hold config.Hold, self string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, known := l.st.Members[name]; known || name == self {
		return false
	}
	l.st.Members[name] = member{Hold: hold}
	return true
}
