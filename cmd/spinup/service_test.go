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
