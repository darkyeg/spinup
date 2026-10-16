package packages

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/darkyeg/spinup/internal/atomicfile"
)

type receipt struct {
	Source  string   `json:"source"`
	Current *Release `json:"current,omitempty"`
	Pending *Release `json:"pending,omitempty"`
}

type receipts struct {
	Version int                `json:"version"`
	Entries map[string]receipt `json:"entries"`
}

func (m Manager) receiptPath(t Target) string {
	return m.lib.Path("local/package-targets/" + targetKey(t) + ".json")
}
func (m Manager) readReceipts(t Target) (receipts, error) {
	r := receipts{Version: 1, Entries: map[string]receipt{}}
	data, err := os.ReadFile(m.receiptPath(t))
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return r, err
	}
	if r.Version != 1 || r.Entries == nil {
		return r, errors.New("invalid package installation receipts")
	}
	for key, e := range r.Entries {
		source, err := sourceID(key)
		if err != nil || source != key || e.Source != key {
			return r, errors.New("invalid package receipt source")
		}
		for _, v := range []*Release{e.Current, e.Pending} {
			if v != nil {
				if err := validateRelease(*v); err != nil {
					return r, err
				}
			}
		}
		if e.Current == nil && e.Pending == nil {
			return r, fmt.Errorf("empty receipt for %s", key)
		}
	}
	return r, nil
}
func (m Manager) writeReceipts(t Target, r receipts) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(m.receiptPath(t), append(data, '\n'), 0o600)
}
