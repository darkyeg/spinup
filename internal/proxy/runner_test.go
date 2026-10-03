package proxy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestCanceledStartDoesNotWantOrSpawnTheProxy(t *testing.T) {
	var r Runner
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled start: %v", err)
	}
	if r.Running() || r.wanted {
		t.Fatal("a canceled start enabled the proxy")
	}
}

func TestCancellationDuringThePortProbePreventsSpawn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cancel()
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	r := Runner{Port: number}
	if err := r.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled probe spawned or attempted a process: %v", err)
	}
	if r.Running() {
		t.Fatal("a canceled port probe launched the proxy")
	}
}
