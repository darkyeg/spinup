package tailnet

import (
	"context"
	"net"
	"net/netip"
	"strings"

	"github.com/darkyeg/spinup/internal/shell"
)

// PathKind is how traffic to a peer flows.
type PathKind int

const (
	NoAnswer PathKind = iota
	LAN
	Direct
	Relay
)

// Path is the route to a peer; Via is the address or relay the last answer came through.
type Path struct {
	Kind PathKind
	Via  string
}

func (p Path) String() string {
	switch p.Kind {
	case LAN:
		return "LAN " + p.Via
	case Direct:
		return "direct " + p.Via
	case Relay:
		return "relay " + p.Via
	}
	return "online, but ping got no answer"
}

// PathTo pings peer five times and reports the route the last answer took.
func (c CLI) PathTo(ctx context.Context, peer string) (Path, error) {
	bin, err := c.bin()
	if err != nil {
		return Path{}, err
	}
	out, _ := shell.Output(ctx, bin, "ping", "-c", "5", "--timeout", "2s", peer)
	return parsePing(out), nil
}

func parsePing(output string) Path {
	var last string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "pong") {
			last = line
		}
	}
	if last == "" {
		return Path{}
	}
	_, afterVia, _ := strings.Cut(last, " via ")
	via, _, _ := strings.Cut(afterVia, " in ")
	via = strings.TrimSpace(via)
	switch {
	case strings.HasPrefix(via, "DERP"):
		return Path{Kind: Relay, Via: via}
	case isLANAddress(via):
		return Path{Kind: LAN, Via: via}
	}
	return Path{Kind: Direct, Via: via}
}

func isLANAddress(endpoint string) bool {
	host, _, err := net.SplitHostPort(endpoint)
	if err != nil {
		return false
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.Is4() && addr.IsPrivate()
}
