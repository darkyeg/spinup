// Package cluster decides which machine holds the accounts, and runs the spinup service.
package cluster

import (
	"fmt"
	"sort"
	"time"

	"github.com/darkyeg/spinup/internal/config"
)

// Peer is another hub/standby machine as this one sees it right now.
type Peer struct {
	Name string
	Role config.Role
	// Reachable: its spinup service answered just now.
	Reachable bool
	// Online: Tailscale's control server sees it (independent of whether we can reach it).
	Online bool
	// OfflineFor: how long Tailscale has reported it offline (0 while online).
	OfflineFor time.Duration
	IsLeader   bool
	Epoch      int64
	// Synced: it is a follower whose logins match the current leader's recently.
	Synced bool
	// MayHaveLed: Tailscale saw it online long enough after this machine last ran that it could
	// have taken over meanwhile, so it may hold newer logins than ours.
	MayHaveLed bool
}

// View is everything one decision needs.
type View struct {
	Self          string
	Role          config.Role
	IsLeader      bool
	Epoch         int64
	KnownLeader   string // the last leader this machine knew of ("" if none ever)
	Peers         []Peer // other hub/standby machines, reachable or not
	FailoverAfter time.Duration
	AutoFailback  bool
	// ForceLead: the user ran `spinup lead --force` (e.g. the other machine is lost for good).
	ForceLead bool
}

// Kind is what to do.
type Kind int

const (
	// Stay: keep the current state.
	Stay Kind = iota
	// Lead: start holding the accounts with Epoch.
	Lead
	// StepDown: stop holding the accounts; Leader (Epoch) holds them.
	StepDown
	// Follow: Leader (Epoch) holds the accounts; keep a synced copy.
	Follow
	// HandOff: move the accounts to Target, the planned way.
	HandOff
	// Wait: do nothing yet; Reason says why.
	Wait
)

func (k Kind) String() string {
	return [...]string{"stay", "lead", "step down", "follow", "hand off", "wait"}[k]
}

// Action is a decision.
type Action struct {
	Kind   Kind
	Leader string
	Epoch  int64
	Target string
	Reason string
}

func (a Action) String() string {
	switch a.Kind {
	case Lead:
		return fmt.Sprintf("lead (epoch %d): %s", a.Epoch, a.Reason)
	case StepDown, Follow:
		return fmt.Sprintf("%s %s (epoch %d)", a.Kind, a.Leader, a.Epoch)
	case HandOff:
		return fmt.Sprintf("hand off to %s: %s", a.Target, a.Reason)
	case Wait:
		return "wait: " + a.Reason
	}
	return a.Kind.String()
}

// claimsAbove reports whether claim (epoch, name) outranks (epoch, name): higher epoch wins, and on
// a tie the alphabetically first name wins, so two leaders always agree on who stays.
func claimsAbove(epoch int64, name string, otherEpoch int64, otherName string) bool {
	return epoch > otherEpoch || (epoch == otherEpoch && name < otherName)
}

// Decide is the whole leadership policy, kept free of I/O so it can be tested exhaustively.
//
// Safety first: a machine only starts holding the accounts when no other machine can be holding
// them. "Can't reach the leader" is never enough on its own; Tailscale's control server must also
// report the leader offline, for FailoverAfter. Otherwise a broken path between two machines
// (bad Wi-Fi) would create two leaders, and two leaders refreshing the same logins log them out.
func Decide(v View) Action {
	if !v.Role.Eligible() {
		return Action{Kind: Stay}
	}
	maxEpoch := v.Epoch
	var claim *Peer
	for i := range v.Peers {
		p := &v.Peers[i]
		if p.Epoch > maxEpoch {
			maxEpoch = p.Epoch
		}
		if p.Reachable && p.IsLeader && (claim == nil || claimsAbove(p.Epoch, p.Name, claim.Epoch, claim.Name)) {
			claim = p
		}
	}

	if v.IsLeader {
		if claim != nil && claimsAbove(claim.Epoch, claim.Name, v.Epoch, v.Self) {
			return Action{Kind: StepDown, Leader: claim.Name, Epoch: claim.Epoch}
		}
		if v.Role == config.RoleStandby && v.AutoFailback {
			for _, p := range v.Peers {
				if p.Role == config.RoleHub && p.Reachable && !p.IsLeader && p.Synced {
					return Action{Kind: HandOff, Target: p.Name, Reason: "the hub is back and synced"}
				}
			}
		}
		return Action{Kind: Stay}
	}

	if claim != nil {
		return Action{Kind: Follow, Leader: claim.Name, Epoch: claim.Epoch}
	}
	if v.ForceLead {
		return Action{Kind: Lead, Epoch: maxEpoch + 1, Reason: "forced by the user"}
	}

	// Nobody we can reach holds the accounts.
	for _, p := range v.Peers {
		if p.Online && !p.Reachable {
			return Action{Kind: Wait, Reason: p.Name + " is online but its spinup service doesn't answer"}
		}
	}

	switch v.KnownLeader {
	case "":
		if v.Role == config.RoleHub {
			return Action{Kind: Lead, Epoch: maxEpoch + 1, Reason: "first start, no other leader"}
		}
		return Action{Kind: Wait, Reason: "no leader yet; the hub starts first"}
	case v.Self:
		// Using logins that another machine has since refreshed would send an outdated refresh
		// token, which providers may answer by revoking the account's newer tokens too.
		for _, p := range v.Peers {
			if p.MayHaveLed {
				return Action{Kind: Wait, Reason: p.Name + " may have taken over while this machine was off; " +
					"waiting for it (or `spinup lead --force`)"}
			}
		}
		return Action{Kind: Lead, Epoch: maxEpoch + 1, Reason: "this machine was leader and nobody took over"}
	}

	var leader *Peer
	for i := range v.Peers {
		if v.Peers[i].Name == v.KnownLeader {
			leader = &v.Peers[i]
		}
	}
	if leader != nil && leader.Online {
		return Action{Kind: Wait, Reason: "leader " + v.KnownLeader + " is online but doesn't answer"}
	}
	if leader != nil && leader.OfflineFor < v.FailoverAfter {
		return Action{Kind: Wait, Reason: fmt.Sprintf("leader %s offline for %s; taking over after %s",
			v.KnownLeader, leader.OfflineFor.Round(time.Second), v.FailoverAfter)}
	}

	// The leader is gone. The best candidate takes over: the hub first, then by name.
	candidates := []Peer{{Name: v.Self, Role: v.Role}}
	for _, p := range v.Peers {
		if p.Reachable && p.Online {
			candidates = append(candidates, p)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if (a.Role == config.RoleHub) != (b.Role == config.RoleHub) {
			return a.Role == config.RoleHub
		}
		return a.Name < b.Name
	})
	if candidates[0].Name != v.Self {
		return Action{Kind: Wait, Reason: "leader is gone; " + candidates[0].Name + " takes over"}
	}
	return Action{Kind: Lead, Epoch: maxEpoch + 1, Reason: "leader " + v.KnownLeader + " is offline"}
}
