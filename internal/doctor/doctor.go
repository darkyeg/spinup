// Package doctor turns facts about this machine into checks, each with the command that fixes it.
package doctor

import (
	"fmt"
	"strings"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/release"
	"github.com/darkyeg/spinup/internal/tailnet"
)

type Level int

const (
	OK Level = iota
	Warn
	Fail
)

type Check struct {
	Level Level
	Label string
	Fix   string
}

type Section struct {
	Title  string
	Checks []Check
}

// Failures counts the checks that need fixing.
func Failures(sections []Section) int {
	n := 0
	for _, s := range sections {
		for _, c := range s.Checks {
			if c.Level == Fail {
				n++
			}
		}
	}
	return n
}

func ok(label string) Check        { return Check{Level: OK, Label: label} }
func warn(label, fix string) Check { return Check{Level: Warn, Label: label, Fix: fix} }
func fail(label, fix string) Check { return Check{Level: Fail, Label: label, Fix: fix} }

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// Tools checks the dev tools in tools.json.
func Tools(missing []string, total int) Section {
	if len(missing) > 0 {
		return Section{"Tools", []Check{fail("missing: "+strings.Join(missing, ", "), "spinup tools")}}
	}
	return Section{"Tools", []Check{ok(fmt.Sprintf("all %d tools in tools.json", total))}}
}

// Network checks Tailscale and how this machine reaches each online peer.
func Network(st tailnet.Status, paths map[string]tailnet.Path, now time.Time) Section {
	s := Section{Title: "Network"}
	if !st.Running {
		s.Checks = append(s.Checks, fail("Tailscale: "+orNot(st.BackendState), "spinup setup <name>"))
		return s
	}
	s.Checks = append(s.Checks, ok(fmt.Sprintf("Tailscale running as %s %s", st.Self.Name, st.Self.IP)))
	for _, n := range st.Machines() {
		switch {
		case n.Expired:
			s.Checks = append(s.Checks, fail(n.Name+": Tailscale login expired", "log in on "+n.Name+": tailscale up"))
		case !n.KeyExpiry.IsZero():
			s.Checks = append(s.Checks, warn(fmt.Sprintf("%s: Tailscale login expires in %d days", n.Name, int(n.KeyExpiry.Sub(now).Hours()/24)),
				"https://login.tailscale.com/admin/machines > ... > Disable key expiry"))
		}
	}
	if len(st.CertDomains) == 0 {
		s.Checks = append(s.Checks, warn("Tailscale HTTPS certificates are off, so `tailscale serve` and T3 Code's pairing hang",
			"https://login.tailscale.com/admin/dns > HTTPS Certificates > Enable"))
	}
	if !st.MagicDNS {
		s.Checks = append(s.Checks, warn("MagicDNS is off, so machine names don't resolve", "https://login.tailscale.com/admin/dns > Enable MagicDNS"))
	}
	for _, p := range st.Peers {
		path, asked := paths[p.Name]
		switch {
		case !p.Online:
			s.Checks = append(s.Checks, ok(p.Name+": offline"))
		case !asked:
		case path.Kind == tailnet.Relay:
			s.Checks = append(s.Checks, warn(p.Name+": "+path.String(), "slower; a strict NAT or firewall blocks the direct path"))
		default:
			s.Checks = append(s.Checks, ok(p.Name+": "+path.String()))
		}
	}
	return s
}

// Service is what doctor knows about the accounts service on this machine.
type Service struct {
	Installed bool
	Report    *api.Report // nil when the service doesn't answer
	Problem   string      // why it doesn't answer
	// ProxyVersion and ProxyLatest are empty when unknown.
	ProxyVersion, ProxyLatest string
	// LoginOnly is set when the service runs only while the user is logged in (a macOS LaunchAgent).
	LoginOnly bool
}

// Accounts checks the accounts service.
func Accounts(svc Service, port int) Section {
	s := Section{Title: "Accounts"}
	switch {
	case !svc.Installed:
		s.Checks = append(s.Checks, fail("the accounts service isn't set up here", "spinup setup <name>"))
		return s
	case svc.Report == nil:
		s.Checks = append(s.Checks, fail("the accounts service doesn't answer: "+svc.Problem, "spinup setup (repairs it)"))
		return s
	}
	r := *svc.Report
	switch {
	case r.Leading && !r.ProxyRunning:
		s.Checks = append(s.Checks, fail("held here, but CLIProxyAPI isn't running", "see spinup.log; spinup setup repairs it"))
	case r.Leading:
		s.Checks = append(s.Checks, ok("held here"))
	case r.Waiting != "":
		s.Checks = append(s.Checks, warn("nobody holds them: "+r.Waiting, "spinup status"))
	case r.Leader != "":
		s.Checks = append(s.Checks, ok("held by "+r.Leader))
	default:
		s.Checks = append(s.Checks, warn("nobody holds them yet", "start the hub, or spinup status"))
	}
	if r.Leading {
		s.Checks = append(s.Checks, loginsCheck(r, port))
	}
	if svc.LoginOnly {
		s.Checks = append(s.Checks, warn("macOS runs the service only while you are logged in, so this machine holds the accounts only then",
			"make an always-on Linux or Windows machine the hub or a standby, or stay logged in"))
	}
	if svc.ProxyLatest != "" && svc.ProxyVersion != "" && svc.ProxyLatest != svc.ProxyVersion {
		s.Checks = append(s.Checks, warn(fmt.Sprintf("CLIProxyAPI %s, latest is %s", svc.ProxyVersion, svc.ProxyLatest), "spinup update"))
	}
	return s
}

func loginsCheck(r api.Report, port int) Check {
	counts := map[string]int{}
	for _, a := range r.Accounts {
		counts[a.Type]++
	}
	label := fmt.Sprintf("logins: %d Claude, %d Codex", counts["claude"], counts["codex"])
	if counts["claude"]+counts["codex"] == 0 {
		return warn(label, fmt.Sprintf("add accounts: http://localhost:%d/management.html > OAuth Login", port))
	}
	return ok(label)
}

// Agents checks the skills and the agent config against spinup's data and your library.
func Agents(notInstalled, unlisted, drifted []string, listed int) Section {
	s := Section{Title: "Agents"}
	var problems []string
	if len(notInstalled) > 0 {
		problems = append(problems, "missing "+strings.Join(notInstalled, ", "))
	}
	if len(unlisted) > 0 {
		problems = append(problems, "unlisted "+strings.Join(unlisted, ", "))
	}
	if len(problems) > 0 {
		s.Checks = append(s.Checks, fail("skills: "+strings.Join(problems, "; "), "spinup skills"))
	} else {
		s.Checks = append(s.Checks, ok(fmt.Sprintf("skills: %d installed, none extra", listed)))
	}
	if len(drifted) > 0 {
		s.Checks = append(s.Checks, fail(fmt.Sprintf("%d agent config %s out of date: %s", len(drifted),
			plural(len(drifted), "file is", "files are"), strings.Join(drifted, ", ")), "spinup agents"))
	} else {
		s.Checks = append(s.Checks, ok("instructions, subagents and settings are as spinup and your library set them"))
	}
	return s
}

// Update checks whether a newer spinup is out.
func Update(running, latest string) Section {
	if !release.Newer(latest, running) {
		return Section{Title: "spinup", Checks: []Check{ok("spinup " + running)}}
	}
	return Section{Title: "spinup", Checks: []Check{warn(fmt.Sprintf("spinup %s, latest is %s", running, latest), "spinup update")}}
}

func orNot(state string) string {
	if state == "" {
		return "not installed"
	}
	return state
}
