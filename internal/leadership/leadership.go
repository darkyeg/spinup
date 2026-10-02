// Package leadership decides which machine holds the accounts. It reads facts and returns a
// decision; it does no I/O, so every rule is tested as a table.
//
// Safety comes first: two machines refreshing the same logins log the accounts out. A machine
// starts holding the accounts only when no other machine can be holding them, and "I can't reach
// the leader" is never enough: Tailscale's control server must report it offline for FailoverAfter.
package leadership

import (
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/darkyeg/spinup/internal/config"
)

// PeerState is what this machine knows about another machine that can hold the accounts.
type PeerState int

const (
	Offline  PeerState = iota // Tailscale reports it offline
	Silent                    // online per Tailscale, but its service doesn't answer
	Standing                  // answers, with a copy of the logins that may be behind
	Synced                    // answers, with the leader's current logins
	Starting                  // answers, and is starting its proxy to hold the accounts
	Leading                   // answers, and holds the accounts
)

var peerStateNames = [...]string{"offline", "silent", "standing", "synced", "starting", "leading"}

func (s PeerState) String() string { return peerStateNames[s] }

func (s PeerState) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

func (s *PeerState) UnmarshalText(text []byte) error {
	for i, name := range peerStateNames {
		if name == string(text) {
			*s = PeerState(i)
			return nil
		}
	}
	return fmt.Errorf("unknown peer state %q", text)
}

func (s PeerState) answers() bool { return s >= Standing }

func (s PeerState) claims() bool { return s >= Starting }

type Peer struct {
	Name  string      `json:"name"`
	Hold  config.Hold `json:"hold"`
	State PeerState   `json:"state"`
	Epoch int64       `json:"epoch"`
	// OfflineFor counts from Tailscale's last sight of an Offline peer.
	OfflineFor time.Duration `json:"offline_for"`
	// MayHaveLed: Tailscale saw it online long enough after this machine last ran that it could
	// have taken the accounts meanwhile, so its logins may be newer than ours.
	MayHaveLed bool `json:"may_have_led"`
}

type View struct {
	Self          string
	Hold          config.Hold
	Leading       bool
	Epoch         int64
	KnownLeader   string
	Peers         []Peer
	FailoverAfter time.Duration
	AutoFailback  bool
	// Forced: the user ran `spinup takeover` because the leader is lost for good.
	Forced bool
	// Pending is set while a hand-off from this machine has an unknown outcome.
	Pending *PendingHandOff
}

// PendingHandOff: this machine sent the accounts to KnownLeader and never learned whether it took them.
type PendingHandOff struct {
	// Age is the time since the request ended; Window is how long a late request can still land.
	Age, Window time.Duration
}

type Kind int

const (
	Stay Kind = iota
	Lead
	StepDown
	Follow
	HandOff
	Wait
)

var kindNames = [...]string{"stay", "lead", "step down", "follow", "hand off", "wait"}

func (k Kind) String() string { return kindNames[k] }

type Decision struct {
	Kind   Kind
	Leader string // Follow, StepDown
	Epoch  int64  // Lead, Follow, StepDown
	Target string // HandOff
	Reason string // Lead, HandOff, Wait
}

func (d Decision) String() string {
	switch d.Kind {
	case Lead:
		return fmt.Sprintf("lead (epoch %d): %s", d.Epoch, d.Reason)
	case StepDown, Follow:
		return fmt.Sprintf("%s %s (epoch %d)", d.Kind, d.Leader, d.Epoch)
	case HandOff:
		return fmt.Sprintf("hand off to %s: %s", d.Target, d.Reason)
	case Wait:
		return "wait: " + d.Reason
	}
	return d.Kind.String()
}

func Decide(v View) Decision {
	if !v.Hold.CanHold() {
		return Decision{Kind: Stay}
	}
	claim := strongestClaim(v.Peers)
	if v.Leading {
		return decideAsLeader(v, claim)
	}
	if claim != nil {
		return Decision{Kind: Follow, Leader: claim.Name, Epoch: claim.Epoch}
	}
	next := nextEpoch(v)
	if v.Forced {
		return Decision{Kind: Lead, Epoch: next, Reason: "forced by the user"}
	}
	for _, p := range v.Peers {
		if p.State == Silent {
			return wait(p.Name + " is online but its spinup service doesn't answer")
		}
	}
	switch v.KnownLeader {
	case "":
		if v.Hold == config.HoldHub {
			return Decision{Kind: Lead, Epoch: next, Reason: "first start, no other leader"}
		}
		return wait("no leader yet; the hub starts first")
	case v.Self:
		return resume(v, next)
	}
	return replaceLeader(v, next)
}

func decideAsLeader(v View, claim *Peer) Decision {
	if claim != nil && outranks(claim.Epoch, claim.Name, v.Epoch, v.Self) {
		return Decision{Kind: StepDown, Leader: claim.Name, Epoch: claim.Epoch}
	}
	if v.Hold == config.HoldStandby && v.AutoFailback {
		for _, p := range v.Peers {
			if p.Hold == config.HoldHub && p.State == Synced {
				return Decision{Kind: HandOff, Target: p.Name, Reason: "the hub is back and synced"}
			}
		}
	}
	return Decision{Kind: Stay}
}

// resume: this machine led last, and logins refreshed elsewhere since would make it send an outdated refresh token.
func resume(v View, next int64) Decision {
	for _, p := range v.Peers {
		if p.MayHaveLed {
			return wait(p.Name + " may have taken over while this machine was off; waiting for it (or `spinup takeover`)")
		}
	}
	return Decision{Kind: Lead, Epoch: next, Reason: "this machine led and nobody took over"}
}

func replaceLeader(v View, next int64) Decision {
	i := slices.IndexFunc(v.Peers, func(p Peer) bool { return p.Name == v.KnownLeader })
	if i < 0 {
		return wait("leader " + v.KnownLeader + " isn't known among the machines that can hold; waiting to see it")
	}
	p := v.Peers[i]
	if v.Pending != nil && p.State.answers() && p.Epoch < v.Epoch {
		return afterUnknownHandOff(v, p, next)
	}
	switch {
	case p.State != Offline:
		return wait("leader " + p.Name + " is online but doesn't hold the accounts")
	case p.OfflineFor < v.FailoverAfter:
		return wait(fmt.Sprintf("leader %s offline for %s; taking over after %s",
			p.Name, p.OfflineFor.Round(time.Second), v.FailoverAfter))
	}
	if best := bestCandidate(v); best != v.Self {
		return wait("the leader is gone; " + best + " takes over")
	}
	return Decision{Kind: Lead, Epoch: next, Reason: "leader " + v.KnownLeader + " is offline"}
}

// afterUnknownHandOff waits out the window in which a late hand-off request could still land.
func afterUnknownHandOff(v View, target Peer, next int64) Decision {
	if v.Pending.Age < v.Pending.Window {
		return wait("not yet sure that " + target.Name + " didn't take the accounts")
	}
	return Decision{Kind: Lead, Epoch: next, Reason: target.Name + " never took the accounts"}
}

// bestCandidate prefers the hub, then the first name, among this machine and answering peers.
func bestCandidate(v View) string {
	candidates := []Peer{{Name: v.Self, Hold: v.Hold}}
	for _, p := range v.Peers {
		if p.State.answers() {
			candidates = append(candidates, p)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if (a.Hold == config.HoldHub) != (b.Hold == config.HoldHub) {
			return a.Hold == config.HoldHub
		}
		return a.Name < b.Name
	})
	return candidates[0].Name
}

func strongestClaim(peers []Peer) *Peer {
	var claim *Peer
	for i := range peers {
		p := &peers[i]
		if p.State.claims() && (claim == nil || outranks(p.Epoch, p.Name, claim.Epoch, claim.Name)) {
			claim = p
		}
	}
	return claim
}

// outranks: the higher epoch wins; on a tie the first name wins, so two leaders agree on who stays.
func outranks(epoch int64, name string, otherEpoch int64, otherName string) bool {
	return epoch > otherEpoch || (epoch == otherEpoch && name < otherName)
}

func nextEpoch(v View) int64 {
	highest := v.Epoch
	for _, p := range v.Peers {
		highest = max(highest, p.Epoch)
	}
	return highest + 1
}

func wait(reason string) Decision { return Decision{Kind: Wait, Reason: reason} }
