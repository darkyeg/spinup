package cluster

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/darkyeg/spinup/internal/config"
)

// Member is another hub/standby machine this one has seen.
type Member struct {
	Role  config.Role `json:"role"`
	Epoch int64       `json:"epoch"`
}

// State survives restarts.
type State struct {
	Epoch int64 `json:"epoch"`
	// Leader is the last machine known to hold the accounts.
	Leader string `json:"leader"`
	// LastAlive is updated while the service runs; after a restart it tells which other machines
	// were online long enough meanwhile to have taken over.
	LastAlive time.Time         `json:"last_alive"`
	Members   map[string]Member `json:"members"`
}

// StatePath is the default state file.
func StatePath() string { return filepath.Join(config.StateDir(), "state.json") }

// LoadState reads state.json (an empty state if there is none).
func LoadState(path string) (State, error) {
	s := State{Members: map[string]Member{}}
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
		s.Members = map[string]Member{}
	}
	return s, nil
}

// SaveState writes state.json.
func SaveState(path string, s State) error {
	data, _ := json.MarshalIndent(s, "", "  ")
	return config.WriteFileAtomic(path, append(data, '\n'), 0o600)
}
