package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestActivityCountsRunningRequestsAndTimesTheQuiet(t *testing.T) {
	var a activity
	if got := a.snapshot(time.Now()); got.InFlight != 0 || got.IdleFor != neverBusy {
		t.Fatalf("before any request: %+v, want idle forever", got)
	}
	end, _ := a.begin()
	second, _ := a.begin()
	if got := a.snapshot(time.Now()).InFlight; got != 2 {
		t.Fatalf("in flight = %d, want 2", got)
	}
	end()
	end()
	second()
	got := a.snapshot(time.Now().Add(time.Minute))
	if got.InFlight != 0 || got.IdleFor < time.Minute {
		t.Fatalf("after both ended: %+v, want 0 running and at least a minute idle", got)
	}
}

func TestPauseWaitsForIdleWhileStillServingRequests(t *testing.T) {
	var a activity
	end, _ := a.begin()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	paused := make(chan error, 1)
	go func() { paused <- a.pause(ctx) }()
	second, ok := a.begin()
	if !ok {
		t.Fatal("waiting for idle turned away a request")
	}
	second()
	select {
	case err := <-paused:
		t.Fatalf("pause ended while a request still runs: %v", err)
	default:
	}
	end()
	if err := <-paused; err != nil {
		t.Fatal(err)
	}
	if _, ok := a.begin(); ok {
		t.Fatal("an idle, paused proxy admitted a request")
	}
	if !a.resume() {
		t.Fatal("an unfenced proxy could not resume")
	}
	end, ok = a.begin()
	if !ok {
		t.Fatal("a resumed proxy turned away a request")
	}
	end()
}

func TestCanceledPauseLeavesRequestsServing(t *testing.T) {
	for _, busy := range []bool{false, true} {
		var a activity
		if busy {
			end, _ := a.begin()
			defer end()
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := a.pause(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled pause: %v", err)
		}
		end, ok := a.begin()
		if !ok {
			t.Fatal("a canceled pause closed admission")
		}
		end()
	}
}

func TestHurryAbortsPauseAndMaintenanceCannotClearIt(t *testing.T) {
	var a activity
	end, _ := a.begin()
	defer end()
	stopping := a.stopRequested()
	paused := make(chan error, 1)
	go func() { paused <- a.pause(context.Background()) }()
	a.hurry()
	a.hurry()
	if _, ok := a.begin(); ok {
		t.Fatal("an urgently fenced proxy admitted a new request")
	}
	select {
	case err := <-paused:
		if !errors.Is(err, errMustStop) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("fencing did not wake the pause")
	}
	select {
	case <-stopping:
	default:
		t.Fatal("fencing did not cancel maintenance startup")
	}
	if a.resume() || !a.mustStop() {
		t.Fatal("maintenance cleared the fence")
	}
	a.open()
	select {
	case <-a.stopRequested():
		t.Fatal("a fresh ownership claim inherited the previous stop signal")
	default:
	}
}

func TestDrainReturnsWhenTheLastRequestEnds(t *testing.T) {
	var a activity
	end, _ := a.begin()
	left := make(chan int)
	go func() { left <- a.drain(context.Background(), time.Minute) }()
	end()
	if got := <-left; got != 0 {
		t.Fatalf("drain left %d running, want 0", got)
	}
}

func TestDrainGivesUpAtTheLimitAndSaysHowManyRun(t *testing.T) {
	var a activity
	a.begin()
	a.begin()
	if got := a.drain(context.Background(), 20*time.Millisecond); got != 2 {
		t.Fatalf("drain left %d running, want 2", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := a.drain(ctx, time.Minute); got != 2 {
		t.Fatalf("a cancelled drain left %d running, want 2", got)
	}
}

func TestAfterADrainNewRequestsWaitUntilTheProxyServesAgain(t *testing.T) {
	var a activity
	a.drain(context.Background(), time.Minute)
	if _, ok := a.begin(); ok {
		t.Fatal("a drained proxy took a new request")
	}
	a.open()
	if _, ok := a.begin(); !ok {
		t.Fatal("the proxy serves again but turned a request away")
	}
}

func TestHurryEndsADrainAtOnce(t *testing.T) {
	var a activity
	a.begin()
	left := make(chan int)
	go func() { left <- a.drain(context.Background(), time.Hour) }()
	a.hurry()
	if got := <-left; got != 1 {
		t.Fatalf("drain left %d running, want 1", got)
	}
	if !a.mustStop() {
		t.Fatal("after hurry the machine should know it must stop")
	}
	a.open()
	if a.mustStop() {
		t.Fatal("serving again should forget the urgent stop")
	}
}
