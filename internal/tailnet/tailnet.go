// Package tailnet reads the tailnet from the tailscale CLI: which machines exist, their IPs, and
// whether Tailscale's control server sees them online.
package tailnet

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/darkyeg/spinup/internal/sysproc"
)

// Node is one machine on the tailnet.
type Node struct {
	Name string // MagicDNS short name, e.g. "office-pc"
	IP   string // Tailscale IPv4
	OS   string
	// Online is the control server's view. Unlike "I can't reach it", this stays true when only
	// the path between two machines is broken, so it is the referee for failover. For this machine
	// it means "connected to the control server".
	Online bool
	// LastSeen is when the control server last saw it (zero while online).
	LastSeen time.Time
}

// Status is the tailnet as this machine sees it.
type Status struct {
	Running bool
	Self    Node
	Peers   []Node
}

// Peer returns the peer called name.
func (s Status) Peer(name string) (Node, bool) {
	for _, p := range s.Peers {
		if p.Name == name {
			return p, true
		}
	}
	return Node{}, false
}

// Source gives the current tailnet status. Tests use a fake.
type Source interface {
	Status(ctx context.Context) (Status, error)
}

// CLI runs `tailscale status --json`.
type CLI struct{ Bin string }

// Find locates the tailscale CLI.
func Find() string {
	if p, err := exec.LookPath("tailscale"); err == nil {
		return p
	}
	for _, p := range []string{
		`C:\Program Files\Tailscale\tailscale.exe`,
		"/opt/homebrew/bin/tailscale", "/usr/local/bin/tailscale",
		"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// ErrNoTailscale means the CLI isn't installed.
var ErrNoTailscale = errors.New("tailscale CLI not found")

type rawNode struct {
	HostName     string
	DNSName      string
	OS           string
	TailscaleIPs []string
	Online       bool
	LastSeen     time.Time
}

// Status implements Source.
func (c CLI) Status(ctx context.Context) (Status, error) {
	bin := c.Bin
	if bin == "" {
		bin = Find()
	}
	if bin == "" {
		return Status{}, ErrNoTailscale
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "status", "--json")
	sysproc.Hide(cmd)
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return Status{}, err
	}
	return Parse(out)
}

// Parse decodes `tailscale status --json`.
func Parse(data []byte) (Status, error) {
	var raw struct {
		BackendState string
		Self         rawNode
		Peer         map[string]rawNode
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Status{}, err
	}
	st := Status{Running: raw.BackendState == "Running", Self: convert(raw.Self)}
	st.Self.Online = st.Running && raw.Self.Online
	for _, p := range raw.Peer {
		if n := convert(p); n.IP != "" {
			st.Peers = append(st.Peers, n)
		}
	}
	return st, nil
}

func convert(r rawNode) Node {
	n := Node{Name: ShortName(r.DNSName, r.HostName), OS: r.OS, Online: r.Online}
	if r.LastSeen.Year() > 1 {
		n.LastSeen = r.LastSeen
	}
	for _, ip := range r.TailscaleIPs {
		if strings.Count(ip, ".") == 3 {
			n.IP = ip
			break
		}
	}
	return n
}

// ShortName is the MagicDNS short name ("office-pc" from "office-pc.tail1234.ts.net."), which is
// what people type; it falls back to the OS hostname.
func ShortName(dnsName, hostName string) string {
	name := dnsName
	if name == "" {
		name = hostName
	}
	name, _, _ = strings.Cut(name, ".")
	return strings.ToLower(name)
}
