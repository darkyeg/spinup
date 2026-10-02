package main

import (
	"testing"

	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/leadership"
)

func TestSyncedHolder(t *testing.T) {
	peer := func(name string, hold config.Hold, state leadership.PeerState) leadership.Peer {
		return leadership.Peer{Name: name, Hold: hold, State: state}
	}
	cases := []struct {
		name  string
		peers []leadership.Peer
		want  string
	}{
		{"nobody", nil, ""},
		{"only machines that are behind", []leadership.Peer{peer("a", config.HoldStandby, leadership.Standing)}, ""},
		{"offline and silent machines", []leadership.Peer{
			peer("a", config.HoldHub, leadership.Offline), peer("b", config.HoldStandby, leadership.Silent)}, ""},
		{"a synced standby", []leadership.Peer{peer("a", config.HoldStandby, leadership.Synced)}, "a"},
		{"the first synced standby", []leadership.Peer{
			peer("a", config.HoldStandby, leadership.Synced), peer("b", config.HoldStandby, leadership.Synced)}, "a"},
		{"the hub over a standby listed before it", []leadership.Peer{
			peer("a", config.HoldStandby, leadership.Synced), peer("z", config.HoldHub, leadership.Synced)}, "z"},
		{"a hub that is behind loses to a synced standby", []leadership.Peer{
			peer("z", config.HoldHub, leadership.Standing), peer("a", config.HoldStandby, leadership.Synced)}, "a"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := syncedHolder(c.peers); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
