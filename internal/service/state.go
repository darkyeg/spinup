package service

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"time"

	"github.com/darkyeg/spinup/internal/atomicfile"
	"github.com/darkyeg/spinup/internal/config"
)

// persisted survives restarts, in state.json.
type persisted struct {
	Epoch int64 `json:"epoch"`
	// Leader is the last machine known to hold the accounts.
	Leader string `json:"leader"`
	// LastAlive tells, after a restart, which machines were online long enough meanwhile to take over.
	LastAlive time.Time `json:"last_alive"`
	// Members are the other machines that can hold the accounts.
	Members map[string]member `json:"members"`
}

type member struct {
	Hold  config.Hold `json:"hold"`
	Epoch int64       `json:"epoch"`
}

func statePath() string { return filepath.Join(config.StateDir(), "state.json") }

func loadState(path string) (persisted, error) {
	s := persisted{Members: map[string]member{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, err
	}
	if s.Members == nil {
		s.Members = map[string]member{}
	}
	return s, nil
}

func (m *Machine) save() {
	m.mu.Lock()
	st := m.state
	st.Members = maps.Clone(m.state.Members)
	m.timers.save = time.Now()
	m.mu.Unlock()
	data, _ := json.MarshalIndent(st, "", "  ")
	if err := atomicfile.Write(m.o.StatePath, append(data, '\n'), 0o600); err != nil {
		m.log.Printf("save state: %v", err)
	}
}

// markAlive records the wall-clock time; it reaches the disk every 15 seconds.
func (m *Machine) markAlive() {
	m.mu.Lock()
	m.state.LastAlive = time.Now().Round(0)
	saveNow := due(&m.timers.save, 15*time.Second)
	m.mu.Unlock()
	if saveNow {
		m.save()
	}
}
