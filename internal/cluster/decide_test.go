package cluster

import (
	"testing"
	"time"

	"github.com/darkyeg/spinup/internal/config"
)

const failover = 3 * time.Minute

func view(self string, role config.Role, peers ...Peer) View {
	return View{Self: self, Role: role, Peers: peers, FailoverAfter: failover, AutoFailback: true}
}

func TestDecide(t *testing.T) {
	hub := func(p Peer) Peer { p.Name, p.Role = "hub", config.RoleHub; return p }
	sb := func(p Peer) Peer { p.Name, p.Role = "sb", config.RoleStandby; return p }

	cases := []struct {
		name string
		v    View
		want Kind
	}{
		{"client never leads", view("c", config.RoleClient), Stay},
		{"fresh hub leads", view("hub", config.RoleHub), Lead},
		{"fresh standby waits for the hub", view("sb", config.RoleStandby), Wait},
		{"standby follows a reachable leader",
			view("sb", config.RoleStandby, hub(Peer{Reachable: true, Online: true, IsLeader: true, Epoch: 1})), Follow},
		{"leader online but unreachable: never take over (partition)",
			func() View {
				v := view("sb", config.RoleStandby, hub(Peer{Online: true}))
				v.KnownLeader, v.Epoch = "hub", 1
				return v
			}(), Wait},
		{"leader offline but not long enough",
			func() View {
				v := view("sb", config.RoleStandby, hub(Peer{OfflineFor: time.Minute}))
				v.KnownLeader, v.Epoch = "hub", 1
				return v
			}(), Wait},
		{"leader offline long enough: take over",
			func() View {
				v := view("sb", config.RoleStandby, hub(Peer{OfflineFor: 5 * time.Minute}))
				v.KnownLeader, v.Epoch = "hub", 1
				return v
			}(), Lead},
		{"another eligible machine online but unreachable: wait",
			func() View {
				v := view("sb", config.RoleStandby, hub(Peer{OfflineFor: time.Hour}),
					Peer{Name: "sb2", Role: config.RoleStandby, Online: true})
				v.KnownLeader = "hub"
				return v
			}(), Wait},
		{"only the best candidate takes over",
			func() View {
				v := view("zz", config.RoleStandby, hub(Peer{OfflineFor: time.Hour}),
					Peer{Name: "aa", Role: config.RoleStandby, Online: true, Reachable: true})
				v.KnownLeader = "hub"
				return v
			}(), Wait},
		{"hub that rebooted resumes when nobody took over",
			func() View {
				v := view("hub", config.RoleHub, sb(Peer{Reachable: true, Online: true, Epoch: 1}))
				v.KnownLeader, v.Epoch = "hub", 1
				return v
			}(), Lead},
		{"hub that rebooted waits if a standby may have led meanwhile",
			func() View {
				v := view("hub", config.RoleHub, sb(Peer{OfflineFor: time.Hour, MayHaveLed: true}))
				v.KnownLeader, v.Epoch = "hub", 1
				return v
			}(), Wait},
		{"force lead overrides waiting",
			func() View {
				v := view("hub", config.RoleHub, sb(Peer{OfflineFor: time.Hour, MayHaveLed: true}))
				v.KnownLeader, v.Epoch, v.ForceLead = "hub", 1, true
				return v
			}(), Lead},
		{"hub that comes back follows the standby that took over",
			func() View {
				v := view("hub", config.RoleHub, sb(Peer{Reachable: true, Online: true, IsLeader: true, Epoch: 2}))
				v.KnownLeader, v.Epoch = "hub", 1
				return v
			}(), Follow},
		{"leader steps down for a higher epoch",
			func() View {
				v := view("hub", config.RoleHub, sb(Peer{Reachable: true, Online: true, IsLeader: true, Epoch: 3}))
				v.IsLeader, v.Epoch, v.KnownLeader = true, 2, "hub"
				return v
			}(), StepDown},
		{"leader keeps leading over a lower epoch",
			func() View {
				v := view("sb", config.RoleStandby, hub(Peer{Reachable: true, Online: true, IsLeader: true, Epoch: 1}))
				v.IsLeader, v.Epoch, v.KnownLeader, v.AutoFailback = true, 2, "sb", false
				return v
			}(), Stay},
		{"standby leader hands back to a synced hub",
			func() View {
				v := view("sb", config.RoleStandby, hub(Peer{Reachable: true, Online: true, Epoch: 2, Synced: true}))
				v.IsLeader, v.Epoch, v.KnownLeader = true, 2, "sb"
				return v
			}(), HandOff},
		{"standby leader keeps the accounts until the hub is synced",
			func() View {
				v := view("sb", config.RoleStandby, hub(Peer{Reachable: true, Online: true, Epoch: 2}))
				v.IsLeader, v.Epoch, v.KnownLeader = true, 2, "sb"
				return v
			}(), Stay},
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
	a := View{Self: "a", Role: config.RoleStandby, IsLeader: true, Epoch: 2,
		Peers: []Peer{{Name: "b", Role: config.RoleStandby, Reachable: true, Online: true, IsLeader: true, Epoch: 2}}}
	b := View{Self: "b", Role: config.RoleStandby, IsLeader: true, Epoch: 2,
		Peers: []Peer{{Name: "a", Role: config.RoleStandby, Reachable: true, Online: true, IsLeader: true, Epoch: 2}}}
	if Decide(a).Kind != Stay || Decide(b).Kind != StepDown {
		t.Errorf("on an epoch tie exactly one leader must step down: a=%v b=%v", Decide(a), Decide(b))
	}
}

func TestLeadEpochIsAboveEverySeenEpoch(t *testing.T) {
	v := view("sb", config.RoleStandby, Peer{Name: "hub", Role: config.RoleHub, OfflineFor: time.Hour, Epoch: 7})
	v.KnownLeader, v.Epoch = "hub", 3
	if got := Decide(v); got.Kind != Lead || got.Epoch != 8 {
		t.Errorf("got %v, want lead with epoch 8", got)
	}
}
