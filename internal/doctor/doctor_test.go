package doctor

import (
	"strings"
	"testing"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/logins"
	"github.com/darkyeg/spinup/internal/tailnet"
)

func levels(s Section) []Level {
	var out []Level
	for _, c := range s.Checks {
		out = append(out, c.Level)
	}
	return out
}

func has(s Section, level Level, text string) bool {
	for _, c := range s.Checks {
		if c.Level == level && strings.Contains(c.Label, text) {
			return true
		}
	}
	return false
}

func TestAccounts(t *testing.T) {
	leading := api.Report{Leading: true, ProxyRunning: true, Accounts: []logins.Summary{{Type: "claude"}, {Type: "codex"}}}
	cases := []struct {
		name  string
		svc   Service
		level Level
		text  string
	}{
		{"not set up", Service{}, Fail, "isn't set up"},
		{"not answering", Service{Installed: true, Problem: "refused"}, Fail, "doesn't answer: refused"},
		{"held here with logins", Service{Installed: true, Report: &leading}, OK, "1 Claude, 1 Codex"},
		{"held here without logins", Service{Installed: true, Report: &api.Report{Leading: true, ProxyRunning: true}}, Warn, "0 Claude, 0 Codex"},
		{"held here but the proxy is down", Service{Installed: true, Report: &api.Report{Leading: true}}, Fail, "isn't running"},
		{"waiting", Service{Installed: true, Report: &api.Report{Waiting: "hub online but silent"}}, Warn, "hub online but silent"},
		{"held elsewhere", Service{Installed: true, Report: &api.Report{Leader: "office-pc"}}, OK, "held by office-pc"},
		{"proxy behind", Service{Installed: true, Report: &leading, ProxyVersion: "1.0", ProxyLatest: "1.1"}, Warn, "latest is 1.1"},
		{"macOS holder", Service{Installed: true, Report: &leading, LoginOnly: true}, Warn, "only while you are logged in"},
		{"proxy version unknown", Service{Installed: true, Report: &leading, ProxyLatest: "1.1"}, OK, "held here"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if s := Accounts(c.svc, 8317); !has(s, c.level, c.text) {
				t.Errorf("no %v check mentioning %q in %+v", c.level, c.text, s.Checks)
			}
		})
	}
	if s := Accounts(Service{Installed: true, Report: &leading, ProxyLatest: "1.1"}, 8317); has(s, Warn, "latest") {
		t.Error("an unknown proxy version must not warn about updates")
	}
}

func TestNetwork(t *testing.T) {
	now := time.Now()
	st := tailnet.Status{Running: true, BackendState: "Running", MagicDNS: true, CertDomains: []string{"x"},
		Self: tailnet.Node{Name: "pc", IsSelf: true},
		Peers: []tailnet.Node{
			{Name: "laptop", Online: true},
			{Name: "phone"},
			{Name: "mac", Online: true, KeyExpiry: now.Add(48 * time.Hour)},
			{Name: "old", Expired: true},
		}}
	s := Network(st, map[string]tailnet.Path{"laptop": {Kind: tailnet.Relay, Via: "DERP(fra)"}}, now)
	for _, want := range []struct {
		level Level
		text  string
	}{
		{OK, "running as pc"}, {Warn, "laptop: relay"}, {OK, "phone: offline"},
		{Warn, "mac: Tailscale login expires in 2 days"}, {Fail, "old: Tailscale login expired"},
	} {
		if !has(s, want.level, want.text) {
			t.Errorf("no %v check mentioning %q in %+v", want.level, want.text, s.Checks)
		}
	}
	if s := Network(tailnet.Status{}, nil, now); !has(s, Fail, "not installed") {
		t.Errorf("no Tailscale must fail: %+v", s.Checks)
	}
	if s := Network(tailnet.Status{Running: true, MagicDNS: false}, nil, now); !has(s, Warn, "HTTPS") || !has(s, Warn, "MagicDNS") {
		t.Errorf("off certificates and MagicDNS must warn: %+v", s.Checks)
	}
}

func TestAgentsToolsUpdate(t *testing.T) {
	if got := levels(Agents(nil, nil, nil, 3)); len(got) != 2 || got[0] != OK || got[1] != OK {
		t.Errorf("all matching: %v", got)
	}
	if s := Agents([]string{"tdd"}, []string{"extra"}, []string{"settings.json"}, 3); Failures([]Section{s}) != 2 {
		t.Errorf("missing, unlisted and drifted must fail: %+v", s.Checks)
	}
	if Failures([]Section{Tools([]string{"rg"}, 12)}) != 1 || Failures([]Section{Tools(nil, 12)}) != 0 {
		t.Error("missing tools fail, none missing passes")
	}
	for _, c := range []struct {
		running, latest string
		level           Level
	}{
		{"v1.0.0", "1.0.0", OK}, {"v1.0.0", "1.1.0", Warn}, {"dev", "1.1.0", OK}, {"v1.0.0", "", OK},
	} {
		if got := Update(c.running, c.latest).Checks[0].Level; got != c.level {
			t.Errorf("Update(%q, %q) = %v, want %v", c.running, c.latest, got, c.level)
		}
	}
}
