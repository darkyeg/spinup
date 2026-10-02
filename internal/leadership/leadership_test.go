package leadership

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/darkyeg/spinup/internal/config"
)

const failover = 3 * time.Minute

func view(self string, hold config.Hold, peers ...Peer) View {
	return View{Self: self, Hold: hold, Peers: peers, FailoverAfter: failover, AutoFailback: true, IdleBeforeHandBack: HandBackIdle}
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

	sentHub := func(state PeerState, epoch int64, age time.Duration) View {
		v := knowing(view("sb", config.HoldStandby, hub(state, epoch)), "hub", 3)
		v.Pending = &PendingHandOff{Age: age, Window: 10 * time.Second}
		return v
	}
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
		{"a unanswered hand-off target that answers without the accounts: wait out the window",
			sentHub(Standing, 2, time.Second), Wait},
		{"a unanswered hand-off target that never took the accounts: lead again",
			sentHub(Synced, 2, time.Minute), Lead},
		{"a hand-off target that is starting its proxy is followed", sentHub(Starting, 3, time.Minute), Follow},
		{"a hand-off target that leads is followed", sentHub(Leading, 3, time.Minute), Follow},
		{"a hand-off target that took the epoch but lost its proxy is waited for",
			sentHub(Standing, 3, time.Minute), Wait},
		{"a hand-off target online but silent is waited for", sentHub(Silent, 2, time.Minute), Wait},
		{"a hand-off target offline too briefly is waited for",
			func() View {
				v := sentHub(Offline, 2, time.Minute)
				v.Peers[0].OfflineFor = time.Minute
				return v
			}(), Wait},
		{"a hand-off target offline long enough is replaced",
			func() View {
				v := sentHub(Offline, 2, time.Minute)
				v.Peers[0].OfflineFor = time.Hour
				return v
			}(), Lead},
		{"a leader steps down for a starting machine with a higher epoch",
			leading(view("hub", config.HoldHub, standby(Starting, 3)), 2), StepDown},
		{"a leader steps down for a higher epoch",
			leading(view("hub", config.HoldHub, standby(Leading, 3)), 2), StepDown},
		{"a leader keeps leading over a lower epoch", noFailback, Stay},
		{"a standby leader hands back to a synced hub once idle long enough",
			withActivity(leading(view("sb", config.HoldStandby, hub(Synced, 2)), 2), 0, HandBackIdle), HandOff},
		{"a standby leader that never served a request hands back at once",
			withActivity(leading(view("sb", config.HoldStandby, hub(Synced, 2)), 2), 0, math.MaxInt64), HandOff},
		{"a standby leader with a request running keeps the accounts",
			withActivity(leading(view("sb", config.HoldStandby, hub(Synced, 2)), 2), 1, time.Hour), Stay},
		{"a standby leader idle too briefly keeps the accounts",
			withActivity(leading(view("sb", config.HoldStandby, hub(Synced, 2)), 2), 0, HandBackIdle-time.Second), Stay},
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

func withActivity(v View, inFlight int, idleFor time.Duration) View {
	v.Activity = Activity{InFlight: inFlight, IdleFor: idleFor}
	return v
}

func TestABusyLeaderSaysWhyItKeepsTheAccounts(t *testing.T) {
	busy := withActivity(leading(view("sb", config.HoldStandby, hub(Synced, 2)), 2), 2, 0)
	if got := Decide(busy).Waiting(); !strings.Contains(got, "requests finish") {
		t.Errorf("a busy leader tells %q, want the reason to name the running requests", got)
	}
	recent := withActivity(busy, 0, time.Second)
	if got := Decide(recent).Waiting(); !strings.Contains(got, HandBackIdle.String()) {
		t.Errorf("a recently busy leader tells %q, want the reason to name the quiet time %s", got, HandBackIdle)
	}
	if got := Decide(withActivity(busy, 0, HandBackIdle)).Waiting(); got != "" {
		t.Errorf("a leader that hands back tells %q, want nothing", got)
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

func TestPeerStateTextRoundTrips(t *testing.T) {
	for state := Offline; state <= Leading; state++ {
		text, _ := state.MarshalText()
		var back PeerState
		if err := back.UnmarshalText(text); err != nil || back != state {
			t.Errorf("%v came back as %v (%v)", state, back, err)
		}
	}
}
