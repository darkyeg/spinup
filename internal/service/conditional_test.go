package service

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/logins"
	"github.com/darkyeg/spinup/internal/tailnet"
)

func TestUnchangedLoginPullTransfersNoBody(t *testing.T) {
	leader := loginMachine(t, &loginProxy{})
	file := loginFile("2026-01-01T00:00:00Z", strings.Repeat("test-token", 1024))
	storeLogin(t, leader, file)
	kept := loginFile("2026-01-01T00:00:00Z", "retained")
	kept.Name = "retained.json"
	storeLogin(t, leader, kept)
	follower := loginMachine(t, &loginProxy{})
	follower.ledger.stopLeading()
	var bodies, totalBytes int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded := httptest.NewRecorder()
		leader.routes(onTailnet).ServeHTTP(recorded, r)
		if r.URL.Path == api.PathLogins && r.Method == http.MethodGet {
			if recorded.Body.Len() > 0 {
				bodies++
			}
			totalBytes += recorded.Body.Len()
		}
		for name, values := range recorded.Header() {
			w.Header()[name] = values
		}
		w.WriteHeader(recorded.Code)
		_, _ = w.Write(recorded.Body.Bytes())
	}))
	defer srv.Close()
	follower.o.PeerAddr = func(string, string) string { return strings.TrimPrefix(srv.URL, "http://") }
	ts := tailnet.Status{Running: true, Self: tailnet.Node{Name: "follower", Online: true},
		Peers: []tailnet.Node{{Name: "hub", IP: "127.0.0.1", Online: true}}}
	follower.tail.set(ts)
	follower.peers.answeredNow("hub", leader.Report())
	pull := func() {
		follower.replica.pullSoon()
		follower.follow(context.Background(), "hub", 1, ts)
	}
	pull()
	initialBytes := totalBytes
	for range 120 {
		pull()
	}
	if bodies != 1 || totalBytes != initialBytes {
		t.Fatalf("idle hour transferred %d bodies / %d bytes; want first body only (%d bytes)", bodies, totalBytes, initialBytes)
	}
	if synced := follower.replica.synced(time.Now()); synced == nil || synced.Epoch != 1 {
		t.Fatal("an unchanged pull did not keep the standby synced")
	}
	if err := os.Remove(filepath.Join(follower.cfg.AuthDir, file.Name)); err != nil {
		t.Fatal(err)
	}
	pull()
	if bodies != 2 {
		t.Fatal("a missing local login was not downloaded again")
	}
	storeLogin(t, leader, loginFile("2026-01-01T01:00:00Z", "refreshed"))
	pull()
	if bodies != 3 {
		t.Fatal("a refreshed login was not downloaded")
	}
	updated, err := os.ReadFile(filepath.Join(follower.cfg.AuthDir, file.Name))
	if err != nil || !bytes.Equal(updated, loginFile("2026-01-01T01:00:00Z", "refreshed").Data) {
		t.Fatal("the standby did not store the refreshed login")
	}
	if err := os.Remove(filepath.Join(leader.cfg.AuthDir, file.Name)); err != nil {
		t.Fatal(err)
	}
	pull()
	if _, err := os.Stat(filepath.Join(follower.cfg.AuthDir, file.Name)); !os.IsNotExist(err) {
		t.Fatal("a login removed by the leader remained on the standby")
	}
	t.Logf("120 unchanged polls: 0 response-body bytes instead of %d", initialBytes*120)
}

func TestConditionalLoginPullStillRequiresAuthentication(t *testing.T) {
	m := loginMachine(t, &loginProxy{})
	storeLogin(t, m, loginFile("2026-01-01T00:00:00Z", "test-token"))
	own, err := m.ownLogins(logins.Everything)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, api.PathLogins, nil)
	r.Header.Set("If-None-Match", own.ETag())
	w := httptest.NewRecorder()
	m.routes(onTailnet).ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized || w.Header().Get("ETag") != "" {
		t.Fatal("an unauthenticated caller learned whether logins are unchanged")
	}
}

func TestUnchangedLoginConfirmationRejectsLocalOrLeadershipChanges(t *testing.T) {
	for _, change := range []string{"local file", "epoch", "leader", "leading"} {
		t.Run(change, func(t *testing.T) {
			m := loginMachine(t, &loginProxy{})
			m.ledger.stopLeading()
			m.ledger.follow("leader", 2)
			storeLogin(t, m, loginFile("2026-01-01T00:00:00Z", "original"))
			tag := m.loginTag("leader", 2)
			switch change {
			case "local file":
				storeLogin(t, m, loginFile("2026-01-01T01:00:00Z", "changed"))
			case "epoch":
				m.ledger.follow("leader", 3)
			case "leader":
				m.ledger.follow("other", 3)
			case "leading":
				m.ledger.beginLeading(3, "hub")
			}
			m.confirmLoginCopy("leader", 2, tag)
			if m.replica.synced(time.Now()) != nil {
				t.Fatal("a stale conditional response confirmed a different login copy or leader")
			}
		})
	}
}
