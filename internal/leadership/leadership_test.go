package leadership

import (
	"testing"
	"time"

	"github.com/darkyeg/spinup/internal/config"
)

const failover = 3 * time.Minute

func view(self string, hold config.Hold, peers ...Peer) View {
	return View{Self: self, Hold: hold, Peers: peers, FailoverAfter: failover, AutoFailback: true}
}

func hub(state PeerState, epoch int64) Peer {
	return Peer{Name: "hub", Hold: config.HoldHub, State: state, Epoch: epoch}
}

func standby(state PeerState, epoch int64) Peer {
	return Peer{Name: "sb", Hold: config.HoldStandby, State: state, Epoch: epoch}
}

func offlineHub(offlineFor time.Duration) Peer {
	p := hub(Offline, 1)
	p.OfflineFor = offlineFor
	return p
}

func knowing(v View, leader string, epoch int64) View {
	v.KnownLeader, v.Epoch = leader, epoch
	return v
}

func leading(v View, epoch int64) View {
	v.Leading, v.Epoch, v.KnownLeader = true, epoch, v.Self
	return v
}

func TestDecide(t *testing.T) {
	sbMayHaveLed := standby(Offline, 1)
	sbMayHaveLed.OfflineFor, sbMayHaveLed.MayHaveLed = time.Hour, true
	forced := knowing(view("hub", config.HoldHub, sbMayHaveLed), "hub", 1)
	forced.Forced = true
	noFailback := leading(view("sb", config.HoldStandby, hub(Leading, 1)), 2)
	noFailback.AutoFailback = false

	cases := []struct {
		name string
		v    View
		want Kind
	}{
		{"a machine that never holds stays", view("c", config.HoldNever), Stay},
		{"a fresh hub leads", view("hub", config.HoldHub), Lead},
		{"a fresh standby waits for the hub", view("sb", config.HoldStandby), Wait},
		{"a standby follows the leader", view("sb", config.HoldStandby, hub(Leading, 1)), Follow},
		{"leader online but unreachable: never take over",
			knowing(view("sb", config.HoldStandby, hub(Silent, 1)), "hub", 1), Wait},
		{"leader online without the accounts: wait",
			knowing(view("sb", config.HoldStandby, hub(Standing, 1)), "hub", 1), Wait},
		{"leader offline, not long enough",
			knowing(view("sb", config.HoldStandby, offlineHub(time.Minute)), "hub", 1), Wait},
		{"a leader missing from the view never counts as gone",
			knowing(view("sb", config.HoldStandby), "hub", 1), Wait},
		{"leader offline long enough: take over",
			knowing(view("sb", config.HoldStandby, offlineHub(5*time.Minute)), "hub", 1), Lead},
		{"another standby online but unreachable: wait",
			knowing(view("sb", config.HoldStandby, offlineHub(time.Hour),
				Peer{Name: "sb2", Hold: config.HoldStandby, State: Silent}), "hub", 1), Wait},
		{"only the best candidate takes over",
			knowing(view("zz", config.HoldStandby, offlineHub(time.Hour),
				Peer{Name: "aa", Hold: config.HoldStandby, State: Standing}), "hub", 1), Wait},
		{"a rebooted hub resumes when nobody took over",
			knowing(view("hub", config.HoldHub, standby(Standing, 1)), "hub", 1), Lead},
		{"a rebooted hub waits when a standby may have led",
			knowing(view("hub", config.HoldHub, sbMayHaveLed), "hub", 1), Wait},
		{"takeover overrides waiting", forced, Lead},
		{"a returning hub follows the standby that took over",
			knowing(view("hub", config.HoldHub, standby(Leading, 2)), "hub", 1), Follow},
		{"a leader steps down for a higher epoch",
			leading(view("hub", config.HoldHub, standby(Leading, 3)), 2), StepDown},
		{"a leader keeps leading over a lower epoch", noFailback, Stay},
		{"a standby leader hands back to a synced hub",
			leading(view("sb", config.HoldStandby, hub(Synced, 2)), 2), HandOff},
		{"a standby leader keeps the accounts until the hub is synced",
			leading(view("sb", config.HoldStandby, hub(Standing, 2)), 2), Stay},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Decide(c.v); got.Kind != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestTwoLeadersAgreeOnOne(t *testing.T) {
	a := leading(view("a", config.HoldStandby, Peer{Name: "b", Hold: config.HoldStandby, State: Leading, Epoch: 2}), 2)
	b := leading(view("b", config.HoldStandby, Peer{Name: "a", Hold: config.HoldStandby, State: Leading, Epoch: 2}), 2)
	a.AutoFailback, b.AutoFailback = false, false
	if Decide(a).Kind != Stay || Decide(b).Kind != StepDown {
		t.Errorf("on an epoch tie exactly one leader must step down: a=%v b=%v", Decide(a), Decide(b))
	}
}

func TestANewLeaderOutranksEveryEpochSeen(t *testing.T) {
	gone := offlineHub(time.Hour)
	gone.Epoch = 7
	if got := Decide(knowing(view("sb", config.HoldStandby, gone), "hub", 3)); got.Kind != Lead || got.Epoch != 8 {
		t.Errorf("got %v, want lead with epoch 8", got)
	}
}
