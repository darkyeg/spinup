package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/logins"
	"github.com/darkyeg/spinup/internal/tailnet"
)

type loginProxy struct {
	running atomic.Bool
	starts  atomic.Int32
	stops   atomic.Int32
	onStop  func()
	onStart func(context.Context) error
}

func (p *loginProxy) Start(ctx context.Context) error {
	p.starts.Add(1)
	if p.onStart != nil {
		if err := p.onStart(ctx); err != nil {
			return err
		}
	}
	p.running.Store(true)
	return nil
}

func TestLoginMergeCancellationBeforeIdleDoesNotInterruptTheProxy(t *testing.T) {
	p := &loginProxy{}
	p.running.Store(true)
	m := loginMachine(t, p)
	local := loginFile("2026-01-01T00:00:00Z", "local")
	storeLogin(t, m, local)
	end, _ := m.activity.begin()
	defer end()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	w := postLogin(m, ctx, api.Logins{Files: []logins.File{loginFile("2026-01-01T01:00:00Z", "incoming")}})
	got, err := os.ReadFile(filepath.Join(m.cfg.AuthDir, local.Name))
	if w.Code != http.StatusConflict || err != nil || !bytes.Equal(got, local.Data) {
		t.Fatal("a deferred merge changed credentials or acknowledged success")
	}
	if p.stops.Load() != 0 || !p.Running() || !m.Report().Leading {
		t.Fatal("a deferred merge interrupted the leader")
	}
	second, ok := m.activity.begin()
	if !ok {
		t.Fatal("a deferred merge closed admission")
	}
	second()
}

func TestLoginMergeRecoversAfterTheCallerCancelsDuringStop(t *testing.T) {
	p := &loginProxy{}
	p.running.Store(true)
	m := loginMachine(t, p)
	storeLogin(t, m, loginFile("2026-01-01T00:00:00Z", "local"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.onStop = cancel
	p.onStart = func(ctx context.Context) error { return ctx.Err() }
	w := postLogin(m, ctx, api.Logins{Files: []logins.File{loginFile("2026-01-01T01:00:00Z", "incoming")}})
	if w.Code != http.StatusOK || !p.Running() || !m.Report().Leading {
		t.Fatal("canceling the sender left the stopped proxy unavailable")
	}
}

func TestLoginMergeFencingCancelsTheRestart(t *testing.T) {
	p := &loginProxy{}
	p.running.Store(true)
	m := loginMachine(t, p)
	storeLogin(t, m, loginFile("2026-01-01T00:00:00Z", "local"))
	starting := make(chan struct{})
	p.onStart = func(ctx context.Context) error {
		close(starting)
		<-ctx.Done()
		return ctx.Err()
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- postLogin(m, context.Background(), api.Logins{Files: []logins.File{loginFile("2026-01-01T01:00:00Z", "incoming")}})
	}()
	select {
	case <-starting:
	case <-time.After(time.Second):
		t.Fatal("the merge did not restart the proxy")
	}
	m.activity.hurry()
	select {
	case w := <-done:
		if w.Code != http.StatusConflict || p.Running() || m.Report().Leading || !m.activity.mustStop() {
			t.Fatal("the credential import resurrected a fenced proxy")
		}
	case <-time.After(time.Second):
		t.Fatal("fencing waited for a stalled proxy startup")
	}
}

func TestLoginMergeFencingDuringStopPreventsImportAndRestart(t *testing.T) {
	p := &loginProxy{}
	p.running.Store(true)
	m := loginMachine(t, p)
	local := loginFile("2026-01-01T00:00:00Z", "local")
	storeLogin(t, m, local)
	p.onStop = m.activity.hurry
	w := postLogin(m, context.Background(), api.Logins{Files: []logins.File{loginFile("2026-01-01T01:00:00Z", "incoming")}})
	got, err := os.ReadFile(filepath.Join(m.cfg.AuthDir, local.Name))
	if w.Code != http.StatusConflict || err != nil || !bytes.Equal(got, local.Data) ||
		p.starts.Load() != 0 || p.Running() || m.Report().Leading {
		t.Fatal("a fenced import wrote credentials or resumed the proxy")
	}
}

func TestLoginMergeServiceShutdownCancelsRecovery(t *testing.T) {
	p := &loginProxy{}
	p.running.Store(true)
	m := loginMachine(t, p)
	storeLogin(t, m, loginFile("2026-01-01T00:00:00Z", "local"))
	serviceDone, starting := make(chan struct{}), make(chan struct{})
	m.serviceDone = serviceDone
	p.onStart = func(ctx context.Context) error {
		close(starting)
		<-ctx.Done()
		return ctx.Err()
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- postLogin(m, context.Background(), api.Logins{Files: []logins.File{loginFile("2026-01-01T01:00:00Z", "incoming")}})
	}()
	select {
	case <-starting:
	case <-time.After(time.Second):
		t.Fatal("the merge did not start recovery")
	}
	close(serviceDone)
	select {
	case w := <-done:
		if w.Code != http.StatusConflict || p.Running() || m.Report().Leading {
			t.Fatal("service shutdown allowed a maintenance restart")
		}
	case <-time.After(time.Second):
		t.Fatal("service shutdown waited for a stalled maintenance restart")
	}
}

func TestLoginMergeFailedRestartReleasesOwnership(t *testing.T) {
	p := &loginProxy{}
	p.running.Store(true)
	m := loginMachine(t, p)
	storeLogin(t, m, loginFile("2026-01-01T00:00:00Z", "local"))
	p.onStart = func(context.Context) error { return errors.New("cannot start") }
	w := postLogin(m, context.Background(), api.Logins{Files: []logins.File{loginFile("2026-01-01T01:00:00Z", "incoming")}})
	if w.Code != http.StatusConflict || p.Running() || m.ledger.view(time.Now()).claiming() {
		t.Fatal("a failed restart still advertised ownership")
	}
	if _, ok := m.activity.begin(); ok {
		t.Fatal("a failed restart reopened the dead proxy")
	}
	p.onStart = nil
	if err := m.lead(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	if !m.Report().Leading || !p.Running() {
		t.Fatal("normal election could not recover after a failed restart")
	}
}

type stubbornLoginProxy struct{ *loginProxy }

func (p stubbornLoginProxy) Stop(func() bool) { p.stops.Add(1) }

func TestLoginMergeRefusesToWriteIfTheProxyDidNotStop(t *testing.T) {
	p := stubbornLoginProxy{&loginProxy{}}
	p.running.Store(true)
	m := loginMachine(t, p)
	local := loginFile("2026-01-01T00:00:00Z", "local")
	storeLogin(t, m, local)
	w := postLogin(m, context.Background(), api.Logins{Files: []logins.File{loginFile("2026-01-01T01:00:00Z", "incoming")}})
	got, err := os.ReadFile(filepath.Join(m.cfg.AuthDir, local.Name))
	if w.Code != http.StatusConflict || err != nil || !bytes.Equal(got, local.Data) || p.starts.Load() != 0 {
		t.Fatal("credentials were written beside a proxy that failed to stop")
	}
	end, ok := m.activity.begin()
	if !ok {
		t.Fatal("the unchanged, running proxy did not resume admission")
	}
	end()
}

func TestLoginMergeRejectsTheEntireBadBatchBeforeStopping(t *testing.T) {
	p := &loginProxy{}
	p.running.Store(true)
	m := loginMachine(t, p)
	w := postLogin(m, context.Background(), api.Logins{Files: []logins.File{
		loginFile("2026-01-01T01:00:00Z", "incoming"), {Name: "../invalid.json", Data: json.RawMessage(`{}`)},
	}})
	if w.Code != http.StatusConflict || p.stops.Load() != 0 {
		t.Fatal("an invalid batch stopped the proxy or was accepted")
	}
	files, err := logins.Read(m.cfg.AuthDir)
	if err != nil || len(files) != 0 {
		t.Fatal("an invalid batch partially imported logins")
	}
}

func TestLoginMergeFromAnOldLeaderCannotRemoveAccountsAfterTakeover(t *testing.T) {
	p := &loginProxy{}
	p.running.Store(true)
	m := loginMachine(t, p)
	kept := loginFile("2026-01-01T00:00:00Z", "kept")
	kept.Name = "kept.json"
	storeLogin(t, m, kept)
	m.transition.Lock()
	m.ledger.stopLeading()
	m.ledger.follow("old-leader", 1)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- postLogin(m, context.Background(), api.Logins{From: "old-leader", Epoch: 1, Complete: true,
			Files: []logins.File{loginFile("2026-01-01T01:00:00Z", "incoming")}})
	}()
	m.ledger.beginLeading(2, "hub")
	m.ledger.finishLeading()
	m.transition.Unlock()
	w := <-done
	if _, err := os.Stat(filepath.Join(m.cfg.AuthDir, kept.Name)); err != nil || w.Code != http.StatusOK {
		t.Fatal("the former leader removed an account from the new leader")
	}
	if m.Report().Synced != nil {
		t.Fatal("an old leader's snapshot marked the new leader as its replica")
	}
}

func TestLoginMergeSerializesConcurrentImports(t *testing.T) {
	p := &loginProxy{}
	p.running.Store(true)
	m := loginMachine(t, p)
	storeLogin(t, m, loginFile("2026-01-01T00:00:00Z", "local"))
	starting, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	p.onStart = func(context.Context) error {
		close(starting)
		<-release
		return nil
	}
	newest := loginFile("2026-01-01T02:00:00Z", "newest")
	first, second := make(chan *httptest.ResponseRecorder, 1), make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- postLogin(m, context.Background(), api.Logins{Files: []logins.File{newest}}) }()
	select {
	case <-starting:
	case <-time.After(time.Second):
		t.Fatal("first import did not reach startup")
	}
	go func() {
		second <- postLogin(m, context.Background(), api.Logins{Files: []logins.File{loginFile("2026-01-01T01:00:00Z", "older")}})
	}()
	once.Do(func() { close(release) })
	if (<-first).Code != http.StatusOK || (<-second).Code != http.StatusOK {
		t.Fatal("a concurrent import failed")
	}
	got, err := os.ReadFile(filepath.Join(m.cfg.AuthDir, newest.Name))
	if err != nil || !bytes.Equal(got, newest.Data) || p.stops.Load() != 1 || p.starts.Load() != 1 {
		t.Fatal("concurrent imports lost the newest copy or restarted for an older one")
	}
}

func TestLoginMergeLetsStreamsFinishAndHoldsRequestsDuringRestart(t *testing.T) {
	tn := newTestTailnet(t)
	hub := tn.add("hub", config.HoldHub)
	tn.start(hub)
	tn.waitFor("hub leads", tn.leaderIs("hub"))
	var releaseOnce sync.Once
	release := hub.proxy.holdStarts()
	t.Cleanup(func() {
		releaseOnce.Do(release)
		hub.proxy.finishStreams()
		tn.shutDown(hub)
	})
	storeLogin(t, hub.m, loginFile("2026-01-01T00:00:00Z", "local"))
	answer := streamThrough(hub.front)
	tn.waitForStream(hub)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	merged := make(chan error, 1)
	go func() {
		_, err := hub.m.mergeLogins(ctx, api.Logins{Files: []logins.File{loginFile("2026-01-01T01:00:00Z", "incoming")}})
		merged <- err
	}()
	tn.waitFor("the import waits for the stream", func() bool {
		return hub.logbook.has("waiting for running requests before importing logins")
	})
	if !strings.Contains(get(hub.front, "/"), "CLI Proxy API Server on hub") {
		t.Fatal("waiting for idle prevented a new request")
	}
	hub.proxy.finishStreams()
	if got := tn.result(answer); got.status != http.StatusOK || got.body != "first last on hub" {
		t.Fatalf("the active stream was cut: status %d", got.status)
	}
	tn.waitFor("the proxy stops before importing", func() bool { return !hub.proxy.Running() })
	if !hub.m.Report().Leading {
		t.Fatal("maintenance released ownership while restarting")
	}
	held := streamThrough(hub.front)
	select {
	case <-held:
		t.Fatal("a held request failed before the proxy restarted")
	default:
	}
	releaseOnce.Do(release)
	select {
	case err := <-merged:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("the import did not finish after the proxy restarted")
	}
	if got := tn.result(held); got.status != http.StatusOK || got.body != "first last on hub" {
		t.Fatalf("the held request failed: status %d", got.status)
	}
	if hub.proxy.cut.Load() != 0 {
		t.Fatal("importing a credential cut an active request")
	}
	tn.assertOneProxyAtATime()
}

func TestLoginMergeRestartFencesBeforeTheStandbyTakesOver(t *testing.T) {
	tn := newTestTailnet(t)
	hub := tn.add("hub", config.HoldHub)
	sb := tn.add("sb", config.HoldStandby)
	sb.keeps = true
	tn.start(hub)
	tn.waitFor("hub leads", tn.leaderIs("hub"))
	storeLogin(t, hub.m, loginFile("2026-01-01T00:00:00Z", "initial"))
	tn.start(sb)
	var releaseOnce sync.Once
	release := hub.proxy.holdStarts()
	t.Cleanup(func() {
		releaseOnce.Do(release)
		hub.proxy.finishStreams()
		sb.proxy.finishStreams()
		tn.shutDown(hub)
		tn.shutDown(sb)
	})
	tn.waitFor("standby has a complete copy", func() bool {
		return sb.m.Report().Synced != nil && refreshToken(sb, "acct.json") == "initial"
	})
	newest := loginFile("2026-01-01T01:00:00Z", "newest")
	storeLogin(t, sb.m, newest)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	merged := make(chan error, 1)
	go func() {
		_, err := hub.m.mergeLogins(ctx, api.Logins{From: "sb", Files: []logins.File{newest}})
		merged <- err
	}()
	tn.waitFor("the hub waits for the proxy restart", func() bool { return !hub.proxy.Running() })
	tn.net.setCutOff("hub", true)
	tn.net.setOnline("hub", false)
	tn.waitFor("the hub fences the maintenance restart", func() bool { return !hub.m.Report().Leading })
	select {
	case err := <-merged:
		if err == nil {
			t.Fatal("the fenced maintenance restart reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("fencing did not cancel the maintenance restart")
	}
	tn.waitFor("the standby takes over", tn.leaderIs("sb"))
	releaseOnce.Do(release)
	if hub.proxy.Running() || refreshToken(sb, newest.Name) != "newest" {
		t.Fatal("the old proxy resumed or takeover lost the newest credential")
	}
	tn.assertOneProxyAtATime()
}

func TestFollowRetriesADeferredOfferBeforeTakingTheCompleteSet(t *testing.T) {
	var offers, pulls atomic.Int32
	remote := loginFile("2026-01-01T01:00:00Z", "remote")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			if offers.Add(1) == 1 {
				writeError(w, http.StatusConflict, "busy")
				return
			}
			writeJSON(w, okBody)
		case http.MethodGet:
			pulls.Add(1)
			writeJSON(w, api.Logins{From: "leader", Epoch: 2, Complete: true, Files: []logins.File{remote}})
		}
	}))
	defer srv.Close()
	m := loginMachine(t, &loginProxy{})
	m.ledger.stopLeading()
	local := loginFile("2026-01-01T00:00:00Z", "local")
	local.Name = "local.json"
	storeLogin(t, m, local)
	m.o.PeerAddr = func(string, string) string { return strings.TrimPrefix(srv.URL, "http://") }
	ts := tailnet.Status{Running: true, Self: tailnet.Node{Name: "hub", Online: true},
		Peers: []tailnet.Node{{Name: "leader", IP: "127.0.0.1", Online: true}}}
	m.tail.set(ts)
	m.peers.answeredNow("leader", api.Report{Name: "leader", Hold: config.HoldHub})
	m.follow(context.Background(), "leader", 2, ts)
	if offers.Load() != 1 || pulls.Load() != 0 || m.Report().Synced != nil {
		t.Fatal("a failed offer was followed by a destructive complete pull")
	}
	if _, err := os.Stat(filepath.Join(m.cfg.AuthDir, local.Name)); err != nil {
		t.Fatal("a refused offer lost the local account")
	}
	if _, err := m.mergeLogins(context.Background(), api.Logins{From: "leader", Epoch: 2, Complete: true,
		Files: []logins.File{remote}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.cfg.AuthDir, local.Name)); err != nil || m.Report().Synced != nil {
		t.Fatal("a leader push removed an unoffered local login or claimed a complete replica")
	}
	m.follow(context.Background(), "leader", 2, ts)
	if offers.Load() != 2 || pulls.Load() != 1 || m.Report().Synced == nil {
		t.Fatal("the deferred offer was not retried before syncing")
	}
}

func (p *loginProxy) Stop(func() bool) {
	p.stops.Add(1)
	if p.onStop != nil {
		p.onStop()
	}
	p.running.Store(false)
}

func (p *loginProxy) Running() bool { return p.running.Load() }

func loginMachine(t *testing.T, p Proxy) *Machine {
	t.Helper()
	cfg := config.Defaults()
	cfg.Hold, cfg.AuthDir = config.HoldHub, t.TempDir()
	m := New(Options{Config: cfg, Proxy: p, Secrets: config.Secrets{ManagementPassword: "test-password"},
		StatePath: filepath.Join(t.TempDir(), "state.json"), Log: log.New(io.Discard, "", 0)})
	m.tail.set(tailnet.Status{Running: true, Self: tailnet.Node{Name: "hub", Online: true}})
	m.ledger.beginLeading(1, "hub")
	m.ledger.finishLeading()
	return m
}

func loginFile(at, token string) logins.File {
	data, _ := json.Marshal(map[string]string{"type": "codex", "last_refresh": at, "refresh_token": token})
	return logins.File{Name: "acct.json", Data: data}
}

func storeLogin(t *testing.T, m *Machine, f logins.File) {
	t.Helper()
	if err := os.MkdirAll(m.cfg.AuthDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.cfg.AuthDir, f.Name), f.Data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func postLogin(m *Machine, ctx context.Context, incoming api.Logins) *httptest.ResponseRecorder {
	data, _ := json.Marshal(incoming)
	r := httptest.NewRequest(http.MethodPost, api.PathLogins, bytes.NewReader(data)).WithContext(ctx)
	r.Header.Set(api.KeyHeader, "test-password")
	w := httptest.NewRecorder()
	m.routes(onTailnet).ServeHTTP(w, r)
	return w
}

func TestLoginMergeRechecksTheFinalProxyRefresh(t *testing.T) {
	p := &loginProxy{}
	p.running.Store(true)
	m := loginMachine(t, p)
	storeLogin(t, m, loginFile("2026-01-01T00:00:00Z", "initial"))
	final := loginFile("2026-01-01T02:00:00Z", "final-refresh")
	p.onStop = func() { storeLogin(t, m, final) }
	w := postLogin(m, context.Background(), api.Logins{From: "standby", Files: []logins.File{
		loginFile("2026-01-01T01:00:00Z", "peer-copy"),
	}})
	got, err := os.ReadFile(filepath.Join(m.cfg.AuthDir, final.Name))
	if w.Code != http.StatusOK || err != nil || !bytes.Equal(got, final.Data) {
		t.Fatalf("the final proxy refresh was lost: status %d, read error %v", w.Code, err)
	}
	if p.stops.Load() != 1 || p.starts.Load() != 1 || !p.Running() {
		t.Fatal("the proxy wasn't stopped before merging and resumed afterward")
	}
}

func TestLoginMergeLeavesOlderCopiesAndTheProxyAlone(t *testing.T) {
	p := &loginProxy{}
	p.running.Store(true)
	m := loginMachine(t, p)
	local := loginFile("2026-01-01T02:00:00Z", "local")
	storeLogin(t, m, local)
	w := postLogin(m, context.Background(), api.Logins{From: "standby", Files: []logins.File{
		loginFile("2026-01-01T01:00:00Z", "peer-copy"),
	}})
	got, err := os.ReadFile(filepath.Join(m.cfg.AuthDir, local.Name))
	if w.Code != http.StatusOK || err != nil || !bytes.Equal(got, local.Data) {
		t.Fatal("an older peer copy changed the local login")
	}
	if p.starts.Load() != 0 || p.stops.Load() != 0 || !p.Running() {
		t.Fatal("an unchanged login interrupted the proxy")
	}
}
