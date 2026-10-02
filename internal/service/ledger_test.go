package service

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestLedger(t *testing.T) *ledger {
	t.Helper()
	l := newLedger(filepath.Join(t.TempDir(), "state.json"))
	if err := l.load(); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestATakeoverExpiresIfThisMachineNeverLeads(t *testing.T) {
	l := newTestLedger(t)
	if err := l.takeover(epoch0); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		after time.Duration
		want  bool
	}{{0, true}, {forcedFor - time.Second, true}, {forcedFor, false}, {time.Hour, false}} {
		if got := l.view(epoch0.Add(c.after)).Forced; got != c.want {
			t.Errorf("forced %v after the takeover: got %v, want %v", c.after, got, c.want)
		}
	}
}

func TestATakeoverEndsWhenThisMachineStopsLeadingOrFollows(t *testing.T) {
	ends := map[string]func(*ledger){
		"it starts leading": func(l *ledger) { l.beginLeading(2, "me") },
		"it releases":       func(l *ledger) { l.stopLeading() },
		"it follows":        func(l *ledger) { l.follow("hub", 3) },
	}
	for name, end := range ends {
		t.Run(name, func(t *testing.T) {
			l := newTestLedger(t)
			if err := l.takeover(epoch0); err != nil {
				t.Fatal(err)
			}
			end(l)
			if l.view(epoch0).Forced {
				t.Error("a takeover must not outlive the situation it was asked in")
			}
		})
	}
}

func TestNoTakeoverWhileThisMachineHoldsTheAccounts(t *testing.T) {
	l := newTestLedger(t)
	l.beginLeading(2, "me")
	if l.takeover(epoch0) == nil {
		t.Error("takeover while starting must be refused")
	}
	l.finishLeading()
	if l.takeover(epoch0) == nil {
		t.Error("takeover while leading must be refused")
	}
	if l.view(epoch0).Forced {
		t.Error("a refused takeover must not leave the machine forced")
	}
}

func TestAClaimThatFailsGoesBackToTheEarlierLeader(t *testing.T) {
	l := newTestLedger(t)
	l.follow("hub", 4)
	previous := l.beginLeading(5, "me")
	if st := l.view(epoch0); !st.Starting || st.Epoch != 5 || st.Leader != "me" {
		t.Fatalf("a claim must be visible before the proxy starts: %+v", st)
	}
	l.abandonLeading(previous)
	if st := l.view(epoch0); st.Starting || st.Epoch != 4 || st.Leader != "hub" {
		t.Errorf("after the failed start: %+v", st)
	}
}

func TestAnUnknownHandOffIsRememberedAcrossRestarts(t *testing.T) {
	l := newTestLedger(t)
	l.handedOffUnknown("sb", 7, epoch0)
	if err := l.save(); err != nil {
		t.Fatal(err)
	}
	again := newLedger(l.path)
	if err := again.load(); err != nil {
		t.Fatal(err)
	}
	st := again.view(epoch0)
	if st.Leader != "sb" || st.Epoch != 7 || st.HandOff == nil || st.HandOff.Target != "sb" {
		t.Errorf("got %+v", st)
	}
	again.follow("sb", 7)
	if again.view(epoch0).HandOff == nil {
		t.Error("following the target keeps the hand-off: its claim may still fail")
	}
	again.follow("hub", 9)
	if again.view(epoch0).HandOff != nil {
		t.Error("following someone else settles the hand-off")
	}
}
