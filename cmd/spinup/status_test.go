package main

import (
	"strings"
	"testing"

	"github.com/darkyeg/spinup/internal/api"
)

func TestTheLeaderStatusCountsRunningRequests(t *testing.T) {
	cases := map[int]string{0: "held here (epoch 3)", 1: "1 request running", 4: "4 requests running"}
	for inFlight, want := range cases {
		line := accountsLine(api.Report{Leading: true, ProxyRunning: true, Epoch: 3, InFlight: inFlight})
		if !strings.Contains(line, want) || (inFlight == 0 && strings.Contains(line, "running")) {
			t.Errorf("%d in flight: %q, want it to contain %q", inFlight, line, want)
		}
	}
}
