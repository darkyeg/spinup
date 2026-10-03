package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/logins"
	"github.com/darkyeg/spinup/internal/tailnet"
)

func TestFailedLoginPushIsRetriedWithoutAnotherRefresh(t *testing.T) {
	var pushes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if pushes.Add(1) == 1 {
			writeError(w, http.StatusConflict, "deferred import")
			return
		}
		writeJSON(w, okBody)
	}))
	defer srv.Close()
	m := loginMachine(t, &loginProxy{})
	storeLogin(t, m, loginFile("2026-01-01T00:00:00Z", "local"))
	m.o.PeerAddr = func(string, string) string { return strings.TrimPrefix(srv.URL, "http://") }
	m.tail.set(tailnet.Status{Running: true, Self: tailnet.Node{Name: "hub", Online: true},
		Peers: []tailnet.Node{{Name: "standby", IP: "127.0.0.1", Online: true}}})
	m.peers.answeredNow("standby", api.Report{Name: "standby", Hold: config.HoldStandby})
	m.pushIfChanged(context.Background())
	m.pushIfChanged(context.Background())
	m.pushIfChanged(context.Background())
	if pushes.Load() != 2 {
		t.Fatal("a refused push was lost or kept retrying after success")
	}
}

func TestAStaleFollowDecisionCannotChangeAnAcquiredClaim(t *testing.T) {
	m := loginMachine(t, &loginProxy{})
	m.follow(context.Background(), "former-leader", 1, tailnet.Status{})
	st := m.ledger.view(time.Now())
	if st.Leader != "hub" || !st.Leading {
		t.Fatal("a stale follow decision changed the new leader's claim")
	}
}

func TestFollowDoesNotOfferAlreadyAcceptedLoginsAgain(t *testing.T) {
	var offers, pulls atomic.Int32
	kept := loginFile("2026-01-01T00:00:00Z", "kept")
	removed := kept
	removed.Name = "removed.json"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			offers.Add(1)
			writeJSON(w, okBody)
			return
		}
		files := []logins.File{kept}
		if pulls.Add(1) == 1 {
			files = append(files, removed)
		}
		writeJSON(w, api.Logins{From: "leader", Epoch: 2, Complete: true, Files: files})
	}))
	defer srv.Close()
	m := loginMachine(t, &loginProxy{})
	m.ledger.stopLeading()
	storeLogin(t, m, kept)
	storeLogin(t, m, removed)
	m.o.PeerAddr = func(string, string) string { return strings.TrimPrefix(srv.URL, "http://") }
	ts := tailnet.Status{Running: true, Self: tailnet.Node{Name: "hub", Online: true},
		Peers: []tailnet.Node{{Name: "leader", IP: "127.0.0.1", Online: true}}}
	m.tail.set(ts)
	m.peers.answeredNow("leader", api.Report{Name: "leader", Hold: config.HoldHub})
	m.follow(context.Background(), "leader", 2, ts)
	m.replica.pullSoon()
	m.follow(context.Background(), "leader", 2, ts)
	if offers.Load() != 1 || pulls.Load() != 2 {
		t.Fatal("an accepted offer was repeated and could restore an account removed by the leader")
	}
	if _, err := os.Stat(filepath.Join(m.cfg.AuthDir, removed.Name)); !os.IsNotExist(err) {
		t.Fatal("the leader's removed account was kept on the follower")
	}
	if _, err := os.Stat(filepath.Join(m.cfg.RemovedDir(), removed.Name)); err != nil {
		t.Fatal("the removed account was not archived")
	}
}
