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
	Name    string // MagicDNS short name, e.g. "office-pc"
	DNSName string // full MagicDNS name, e.g. "office-pc.tail1234.ts.net"
	IP      string // Tailscale IPv4
	OS      string
	// Online is the control server's view. Unlike "I can't reach it", this stays true when only
	// the path between two machines is broken, so it is the referee for failover. For this machine
	// it means "connected to the control server".
	Online bool
	// LastSeen is when the control server last saw it (zero while online).
	LastSeen time.Time
	// KeyExpiry is when the machine's login lapses; zero when the key never expires.
	KeyExpiry time.Time
	// Expired means the login has already lapsed, so the machine is off the tailnet until someone logs in on it.
	Expired bool
	IsSelf  bool
}

// Status is the tailnet as this machine sees it.
type Status struct {
	Running      bool
	BackendState string
	Self         Node
	Peers        []Node
	// CertDomains is empty while HTTPS certificates are off for the tailnet.
	CertDomains []string
	MagicDNS    bool
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

// errNoTailscale means the CLI isn't installed.
var errNoTailscale = errors.New("tailscale CLI not found")

type rawNode struct {
	HostName     string
	DNSName      string
	OS           string
	TailscaleIPs []string
	Online       bool
	LastSeen     time.Time
	KeyExpiry    time.Time
	Expired      bool
}

func (c CLI) bin() (string, error) {
	if c.Bin != "" {
		return c.Bin, nil
	}
	if bin := Find(); bin != "" {
		return bin, nil
	}
	return "", errNoTailscale
}

// Status implements Source.
func (c CLI) Status(ctx context.Context) (Status, error) {
	bin, err := c.bin()
	if err != nil {
		return Status{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "status", "--json")
	sysproc.Hide(cmd)
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return Status{}, err
	}
	return parse(out)
}

// parse decodes `tailscale status --json`.
func parse(data []byte) (Status, error) {
	var raw struct {
		BackendState   string
		Self           rawNode
		Peer           map[string]rawNode
		CertDomains    []string
		CurrentTailnet struct{ MagicDNSEnabled bool }
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Status{}, err
	}
	st := Status{
		Running:      raw.BackendState == "Running",
		BackendState: raw.BackendState,
		Self:         convert(raw.Self),
		CertDomains:  raw.CertDomains,
		MagicDNS:     raw.CurrentTailnet.MagicDNSEnabled,
	}
	st.Self.IsSelf = true
	st.Self.Online = st.Running && raw.Self.Online
	for _, p := range raw.Peer {
		if n := convert(p); n.IP != "" {
			st.Peers = append(st.Peers, n)
		}
	}
	return st, nil
}

func convert(r rawNode) Node {
	n := Node{
		Name:    shortName(r.DNSName, r.HostName),
		DNSName: strings.TrimSuffix(strings.ToLower(r.DNSName), "."),
		OS:      r.OS,
		Online:  r.Online,
		Expired: r.Expired,
	}
	if r.LastSeen.Year() > 1 {
		n.LastSeen = r.LastSeen
	}
	if r.KeyExpiry.Year() > 1 {
		n.KeyExpiry = r.KeyExpiry
	}
	for _, ip := range r.TailscaleIPs {
		if strings.Count(ip, ".") == 3 {
			n.IP = ip
			break
		}
	}
	return n
}

// shortName is the MagicDNS short name ("office-pc"), or the OS hostname's.
func shortName(dnsName, hostName string) string {
	name := dnsName
	if name == "" {
		name = hostName
	}
	name, _, _ = strings.Cut(name, ".")
	return strings.ToLower(name)
}
