// Package api is what spinup services and the spinup command say to each other over HTTP.
package api

import (
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/leadership"
	"github.com/darkyeg/spinup/internal/logins"
)

const (
	// KeyHeader carries the management password.
	KeyHeader = "X-Spinup-Key"
	// ForwardedHeader names the sending machine; a request carrying it is never forwarded again.
	ForwardedHeader = "X-Spinup-Forwarded"
	// HoldHeader lets machines learn about each other the moment one calls another.
	HoldHeader = "X-Spinup-Hold"
)

const (
	PathLeader   = "/spinup/leader"   // public; proves what its machine knows
	PathState    = "/spinup/state"    // keyed between machines, open on localhost
	PathLogins   = "/spinup/logins"   // GET the complete set, POST some to merge
	PathSecrets  = "/spinup/secrets"  // for a new standby
	PathReceive  = "/spinup/receive"  // the leader hands this machine the accounts
	PathHandoff  = "/spinup/handoff"  // the user moves the accounts
	PathTakeover = "/spinup/takeover" // the user makes this machine lead; localhost only
	PathStop     = "/spinup/stop"     // the user stops this service gracefully; localhost only
	PathRestart  = "/spinup/restart"  // restart the proxy after an update, if this machine holds the accounts; localhost only
)

// Leader is public: it names who holds the accounts, with proofs of the secrets its machine knows.
type Leader struct {
	Name string      `json:"name"`
	Hold config.Hold `json:"hold"`
	// Leading and Starting both claim the accounts: Starting is a proxy that is coming up.
	Leading       bool   `json:"leading"`
	Starting      bool   `json:"starting,omitempty"`
	Leader        string `json:"leader"`
	Epoch         int64  `json:"epoch"`
	APIKeyProof   string `json:"api_key_proof,omitempty"`
	PasswordProof string `json:"password_proof,omitempty"`
}

type Logins struct {
	From  string `json:"from"`
	Epoch int64  `json:"epoch"`
	// Complete: Files is everything the sender has, so a login missing from it was removed there.
	Complete bool          `json:"complete"`
	Files    []logins.File `json:"files"`
}

type Handoff struct {
	To string `json:"to"`
}

// Report is a machine's state, for the other machines and for `spinup status`.
type Report struct {
	Name         string            `json:"name"`
	Hold         config.Hold       `json:"hold"`
	Version      string            `json:"version"`
	Leading      bool              `json:"leading"`
	Starting     bool              `json:"starting,omitempty"`
	Epoch        int64             `json:"epoch"`
	Leader       string            `json:"leader"`
	LeaderAddr   string            `json:"leader_addr,omitempty"`
	Synced       *Sync             `json:"synced,omitempty"`
	ProxyRunning bool              `json:"proxy_running"`
	Waiting      string            `json:"waiting,omitempty"`
	Accounts     []logins.Summary  `json:"accounts,omitempty"`
	Peers        []leadership.Peer `json:"peers,omitempty"`
}

// Sync is this machine's last complete copy of the leader's logins. The age is measured here, so
// machines with different clocks still agree on it.
type Sync struct {
	Epoch      int64 `json:"epoch"`
	SecondsAgo int   `json:"seconds_ago"`
}

// Error is the body of every failed request.
type Error struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}
