package service

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

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
	// HandOff is the last hand-off whose outcome this machine never learned.
	HandOff *unconfirmedHandOff `json:"unconfirmed_handoff,omitempty"`
}

type member struct {
	Hold  config.Hold `json:"hold"`
	Epoch int64       `json:"epoch"`
}

type unconfirmedHandOff struct {
	Target string    `json:"target"`
	At     time.Time `json:"at"`
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
