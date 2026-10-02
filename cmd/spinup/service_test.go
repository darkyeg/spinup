package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/tailnet"
)

func TestSetupHold(t *testing.T) {
	cases := []struct {
		name string
		cmd  setupCmd
		want config.Hold
	}{
		{"no flag keeps the machine's hold", setupCmd{}, ""},
		{"hub", setupCmd{Hub: true}, config.HoldHub},
		{"standby", setupCmd{Standby: true}, config.HoldStandby},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.cmd.hold(); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestHoldPhrase(t *testing.T) {
	cases := map[config.Hold]string{
		config.HoldHub:     "hub",
		config.HoldStandby: "standby",
		config.HoldNever:   "uses the accounts",
		"":                 "uses the accounts",
	}
	for hold, want := range cases {
		if got := holdPhrase(hold); !strings.Contains(got, want) {
			t.Errorf("holdPhrase(%q) = %q, want it to mention %q", hold, got, want)
		}
	}
}

func TestTailscaleProblem(t *testing.T) {
	cases := []struct {
		name     string
		ts       tailnet.Status
		err      error
		wantText string
	}{
		{"fine", tailnet.Status{Running: true}, nil, ""},
		{"the CLI failed", tailnet.Status{}, errors.New("exec: not found"), "exec: not found"},
		{"installed but stopped", tailnet.Status{}, nil, "not running"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := tailscaleProblem(c.ts, c.err)
			switch {
			case c.wantText == "" && err != nil:
				t.Errorf("unexpected %v", err)
			case c.wantText != "" && (err == nil || !strings.Contains(err.Error(), c.wantText)):
				t.Errorf("got %v, want it to contain %q", err, c.wantText)
			case err != nil && strings.Contains(err.Error(), "<nil>"):
				t.Errorf("the message prints a nil error: %v", err)
			}
		})
	}
}

func TestAMachineReachesTheAccountsOnlyThroughAProvenLeader(t *testing.T) {
	cases := []struct {
		name   string
		report api.Report
		want   bool
	}{
		{"no leader", api.Report{}, false},
		{"leader known but not proven reachable", api.Report{Leader: "hub"}, false},
		{"leader with an address", api.Report{Leader: "hub", LeaderAddr: "100.1.1.1:8317"}, true},
	}
	for _, c := range cases {
		if got := reachesLeader(c.report); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func TestPollUntilStopsAsSoonAsTheConditionHolds(t *testing.T) {
	calls := 0
	if !pollUntil(time.Minute, func() bool { calls++; return calls == 2 }) || calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
	if pollUntil(0, func() bool { return false }) {
		t.Fatal("a false condition cannot succeed")
	}
}

func TestWhenSetupRestartsTheService(t *testing.T) {
	cases := []struct {
		name string
		f    serviceFacts
		want string
	}{
		{"running, same binary, same settings", serviceFacts{boot: answering}, ""},
		{"not registered at boot", serviceFacts{boot: notRegistered}, "registered"},
		{"registered but silent", serviceFacts{boot: registeredSilent}, "answering"},
		{"a different binary", serviceFacts{boot: answering, binaryChanged: true}, "differs"},
		{"changed settings", serviceFacts{boot: answering, configChanged: true}, "settings"},
		{"silent wins over changes", serviceFacts{boot: registeredSilent, binaryChanged: true, configChanged: true}, "answering"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := restartReason(c.f)
			if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
				t.Errorf("restartReason(%+v) = %q, want it to mention %q", c.f, got, c.want)
			}
		})
	}
}

func TestStoredSettingsDifferWhenTheKeysChange(t *testing.T) {
	t.Setenv("SPINUP_HOME", t.TempDir())
	cfg := config.Defaults()
	cfg.ProxyDir = t.TempDir()
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveSecrets(cfg, config.Secrets{APIKey: "a", ManagementPassword: "m"}); err != nil {
		t.Fatal(err)
	}
	before := storedSettings()
	if before != storedSettings() {
		t.Fatal("unchanged files must give equal settings")
	}
	if err := config.SaveSecrets(cfg, config.Secrets{APIKey: "b", ManagementPassword: "m"}); err != nil {
		t.Fatal(err)
	}
	if before == storedSettings() {
		t.Fatal("a new API key must count as a change")
	}
}
