package service

import (
	"testing"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/leadership"
	"github.com/darkyeg/spinup/internal/tailnet"
)

var epoch0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func TestOfflineFacts(t *testing.T) {
	in := sighting{now: epoch0, bootAlive: epoch0.Add(-time.Hour), failover: 3 * time.Minute}
	cases := []struct {
		name      string
		firstSeen time.Time
		node      tailnet.Node
		inTailnet bool
		in        sighting
		wantFor   time.Duration
		wantLed   bool
	}{
		{"counts from the first sight when Tailscale has none", epoch0.Add(-time.Minute),
			tailnet.Node{}, true, in, time.Minute, false},
		{"counts from Tailscale's last sight when it is earlier", epoch0.Add(-time.Minute),
			tailnet.Node{LastSeen: epoch0.Add(-time.Hour)}, true, in, time.Hour, false},
		{"ignores a last sight after the first sight", epoch0.Add(-time.Hour),
			tailnet.Node{LastSeen: epoch0.Add(-time.Minute)}, true, in, time.Hour, true},
		{"online after this machine was last alive long enough to have led", epoch0,
			tailnet.Node{LastSeen: epoch0.Add(-10 * time.Minute)}, true, in, 10 * time.Minute, true},
		{"online only briefly after this machine was last alive", epoch0,
			tailnet.Node{LastSeen: epoch0.Add(-time.Hour + time.Minute)}, true, in, time.Hour - time.Minute, false},
		{"a machine missing from the tailnet may not have led", epoch0,
			tailnet.Node{LastSeen: epoch0.Add(-time.Minute)}, false, in, 0, false},
		{"without a record of being alive nobody may have led", epoch0,
			tailnet.Node{LastSeen: epoch0.Add(-time.Minute)}, true,
			sighting{now: epoch0, failover: 3 * time.Minute}, time.Minute, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotFor, gotLed := offlineFacts(c.firstSeen, c.node, c.inTailnet, c.in)
			if gotFor != c.wantFor || gotLed != c.wantLed {
				t.Errorf("got (%v, %v), want (%v, %v)", gotFor, gotLed, c.wantFor, c.wantLed)
			}
		})
	}
}

func TestPeerState(t *testing.T) {
	synced := func(epoch int64, seconds int) *api.Sync { return &api.Sync{Epoch: epoch, SecondsAgo: seconds} }
	cases := []struct {
		name string
		r    api.Report
		want leadership.PeerState
	}{
		{"leading", api.Report{Leading: true}, leadership.Leading},
		{"leading beats starting", api.Report{Leading: true, Starting: true}, leadership.Leading},
		{"starting", api.Report{Starting: true}, leadership.Starting},
		{"a fresh copy of the current epoch", api.Report{Synced: synced(4, 30)}, leadership.Synced},
		{"a stale copy", api.Report{Synced: synced(4, 300)}, leadership.Standing},
		{"a copy of another epoch", api.Report{Synced: synced(3, 1)}, leadership.Standing},
		{"a copy from the future", api.Report{Synced: synced(4, -5)}, leadership.Standing},
		{"no copy", api.Report{}, leadership.Standing},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := peerState(c.r, 4); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestDescribePeers(t *testing.T) {
	ts := tailnet.Status{
		Self: tailnet.Node{Name: "me"},
		Peers: []tailnet.Node{
			{Name: "hub", Online: true}, {Name: "silent", Online: true},
			{Name: "gone", LastSeen: epoch0.Add(-time.Hour)},
		},
	}
	in := sighting{
		now: epoch0, tailnet: ts, epoch: 2, failover: time.Minute,
		members: map[string]member{
			"me": {Hold: config.HoldStandby}, "hub": {Hold: config.HoldHub, Epoch: 1},
			"silent": {Hold: config.HoldStandby}, "gone": {Hold: config.HoldStandby, Epoch: 1},
			"unlisted": {Hold: config.HoldStandby},
		},
		answered: map[string]api.Report{"hub": {Leading: true, Epoch: 2}},
	}
	offlineSince := map[string]time.Time{"hub": epoch0}
	got := describePeers(in, offlineSince)

	want := []leadership.Peer{
		{Name: "gone", Hold: config.HoldStandby, State: leadership.Offline, Epoch: 1, OfflineFor: time.Hour},
		{Name: "hub", Hold: config.HoldHub, State: leadership.Leading, Epoch: 2},
		{Name: "silent", Hold: config.HoldStandby, State: leadership.Silent},
		{Name: "unlisted", Hold: config.HoldStandby, State: leadership.Offline},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("peer %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
	if _, stale := offlineSince["hub"]; stale {
		t.Error("a machine that answers is no longer counted as offline")
	}
	if _, counted := offlineSince["gone"]; !counted {
		t.Error("an offline machine must be remembered from its first sight")
	}
}

func TestSyncedTargetPrefersTheHubThenTheFirstName(t *testing.T) {
	fresh := &api.Sync{Epoch: 5, SecondsAgo: 1}
	cases := []struct {
		name    string
		reports map[string]api.Report
		want    string
	}{
		{"nobody answers", nil, ""},
		{"a standby that is behind is no target",
			map[string]api.Report{"sb": {Hold: config.HoldStandby}}, ""},
		{"a synced standby", map[string]api.Report{"sb": {Hold: config.HoldStandby, Synced: fresh}}, "sb"},
		{"the hub over a standby",
			map[string]api.Report{"a": {Hold: config.HoldStandby, Synced: fresh}, "z": {Hold: config.HoldHub, Synced: fresh}}, "z"},
		{"the first name among standbys",
			map[string]api.Report{"b": {Hold: config.HoldStandby, Synced: fresh}, "a": {Hold: config.HoldStandby, Synced: fresh}}, "a"},
		{"a machine already leading is no target",
			map[string]api.Report{"sb": {Hold: config.HoldStandby, Leading: true, Synced: fresh}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := syncedTarget(c.reports, 5); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestPendingHandOffAppliesOnlyToItsTarget(t *testing.T) {
	at := epoch0.Add(-5 * time.Second)
	cases := []struct {
		name string
		st   standing
		want *leadership.PendingHandOff
	}{
		{"none", standing{Leader: "sb"}, nil},
		{"a hand-off to the known leader", standing{Leader: "sb", HandOff: &unconfirmedHandOff{Target: "sb", At: at}},
			&leadership.PendingHandOff{Age: 5 * time.Second, Window: time.Second}},
		{"a hand-off to someone who is no longer the leader",
			standing{Leader: "hub", HandOff: &unconfirmedHandOff{Target: "sb", At: at}}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := pendingHandOff(c.st, epoch0, time.Second)
			if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestACutOffLeaderStopsAtHalfTheFailoverTime(t *testing.T) {
	for _, c := range []struct {
		elapsed time.Duration
		want    bool
	}{{0, false}, {89 * time.Second, false}, {90 * time.Second, true}, {time.Hour, true}} {
		if got := cutOffTooLong(c.elapsed, 3*time.Minute); got != c.want {
			t.Errorf("cut off for %v: got %v, want %v", c.elapsed, got, c.want)
		}
	}
}
