package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
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
	"github.com/darkyeg/spinup/internal/atomicfile"
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/leadership"
	"github.com/darkyeg/spinup/internal/tailnet"
)

// fakeTailnet is Tailscale's control-server view, plus broken paths it can't see.
type fakeTailnet struct {
	mu       sync.Mutex
	online   map[string]bool
	lastSeen map[string]time.Time
	peerAPI  map[string]string
	cutOff   map[string]bool // online, but nobody can reach it
}

func (n *fakeTailnet) setOnline(name string, online bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !online && n.online[name] {
		n.lastSeen[name] = time.Now()
	}
	n.online[name] = online
}

func (n *fakeTailnet) setCutOff(name string, cut bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.cutOff[name] = cut
}

func (n *fakeTailnet) addr(name string) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.cutOff[name] {
		return "127.0.0.1:1"
	}
	return n.peerAPI[name]
}

type seenFrom struct {
	net  *fakeTailnet
	self string
}

func (s seenFrom) Status(context.Context) (tailnet.Status, error) {
	s.net.mu.Lock()
	defer s.net.mu.Unlock()
	st := tailnet.Status{Running: true, Self: tailnet.Node{Name: s.self, IP: "127.0.0.1", Online: true}}
	for name, online := range s.net.online {
		if name != s.self {
			st.Peers = append(st.Peers, tailnet.Node{Name: name, IP: "127.0.0.1", Online: online, LastSeen: s.net.lastSeen[name]})
		}
	}
	return st, nil
}

// fakeProxy answers with its machine's name and counts proxies running at once, across machines.
type fakeProxy struct {
	machine string
	port    int
	running *atomic.Int32
	most    *atomic.Int32
	mu      sync.Mutex
	srv     *http.Server
	gate    chan struct{}
}

func (p *fakeProxy) holdStarts() (release func()) {
	p.mu.Lock()
	p.gate = make(chan struct{})
	gate := p.gate
	p.mu.Unlock()
	return func() { close(gate) }
}

func (p *fakeProxy) Start(ctx context.Context) error {
	p.mu.Lock()
	gate := p.gate
	p.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.srv != nil {
		return nil
	}
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p.port))
	if err != nil {
		return err
	}
	p.srv = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "CLI Proxy API Server on %s", p.machine)
	})}
	go p.srv.Serve(l)
	now := p.running.Add(1)
	for most := p.most.Load(); now > most && !p.most.CompareAndSwap(most, now); most = p.most.Load() {
	}
	return nil
}

func (p *fakeProxy) Stop(func() bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.srv != nil {
		p.srv.Close()
		p.srv = nil
		p.running.Add(-1)
	}
}

func (p *fakeProxy) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.srv != nil
}

type testMachine struct {
	name    string
	hold    config.Hold
	dir     string
	front   string
	peerAPI string
	m       *Machine
	proxy   *fakeProxy
	stop    context.CancelFunc
	done    chan struct{}
}

type testTailnet struct {
	t       *testing.T
	net     *fakeTailnet
	running atomic.Int32
	most    atomic.Int32
	all     map[string]*testMachine
}

func newTestTailnet(t *testing.T) *testTailnet {
	return &testTailnet{t: t, all: map[string]*testMachine{}, net: &fakeTailnet{
		online: map[string]bool{}, lastSeen: map[string]time.Time{}, peerAPI: map[string]string{}, cutOff: map[string]bool{},
	}}
}

func (tn *testTailnet) add(name string, hold config.Hold) *testMachine {
	tm := &testMachine{name: name, hold: hold, dir: tn.t.TempDir(), front: freeAddr(tn.t), peerAPI: freeAddr(tn.t)}
	tn.all[name] = tm
	tn.net.mu.Lock()
	tn.net.peerAPI[name] = tm.peerAPI
	tn.net.mu.Unlock()
	return tm
}

func (tn *testTailnet) start(tm *testMachine) {
	cfg := config.Defaults()
	cfg.Hold, cfg.FailoverAfterSeconds = tm.hold, 1
	cfg.ProxyPort = freePort(tn.t)
	cfg.AuthDir = filepath.Join(tm.dir, "auth")
	tm.proxy = &fakeProxy{machine: tm.name, port: cfg.ProxyPort, running: &tn.running, most: &tn.most}
	o := Options{
		Config: cfg, Tailnet: seenFrom{tn.net, tm.name}, Version: "test",
		Secrets:   config.Secrets{APIKey: "k"},
		Log:       log.New(testLog{tn.t, tm.name}, "", 0),
		StatePath: filepath.Join(tm.dir, "state.json"), FrontListen: tm.front, PeerListen: tm.peerAPI,
		Tick:     50 * time.Millisecond,
		PeerAddr: func(name, _ string) string { return tn.net.addr(name) },
	}
	if tm.hold.CanHold() {
		o.Proxy, o.Secrets.ManagementPassword = tm.proxy, "m"
	}
	tm.m = New(o)
	ctx, stop := context.WithCancel(context.Background())
	tm.stop, tm.done = stop, make(chan struct{})
	tn.net.setOnline(tm.name, true)
	go func() {
		defer close(tm.done)
		if err := tm.m.Run(ctx); err != nil {
			tn.t.Errorf("%s: %v", tm.name, err)
		}
	}()
}

// crash stops a machine with no hand-off, like a power cut.
func (tn *testTailnet) crash(tm *testMachine) {
	tm.m.ledger.stopLeading()
	tm.proxy.Stop(nil)
	tn.shutDown(tm)
}

func (tn *testTailnet) shutDown(tm *testMachine) {
	tm.stop()
	<-tm.done
	tn.net.setOnline(tm.name, false)
}

func (tn *testTailnet) leaders() []string {
	var names []string
	for name, tm := range tn.all {
		if tm.m != nil && tm.m.Report().Leading {
			names = append(names, name)
		}
	}
	return names
}

func (tn *testTailnet) waitFor(what string, done func() bool) {
	tn.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	tn.t.Fatalf("timed out waiting for: %s (leaders: %v)", what, tn.leaders())
}

func (tn *testTailnet) leaderIs(name string) func() bool {
	return func() bool { l := tn.leaders(); return len(l) == 1 && l[0] == name }
}

func writeLogin(t *testing.T, tm *testMachine, name, token string) {
	t.Helper()
	data := fmt.Sprintf(`{"type":"codex","refresh_token":%q,"last_refresh":%q}`, token, time.Now().UTC().Format(time.RFC3339Nano))
	if err := atomicfile.Write(filepath.Join(tm.m.cfg.AuthDir, name), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func refreshToken(tm *testMachine, name string) string {
	data, _ := os.ReadFile(filepath.Join(tm.m.cfg.AuthDir, name))
	var login struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.Unmarshal(data, &login)
	return login.RefreshToken
}

func get(addr, path string) string {
	resp, err := http.Get("http://" + addr + path)
	if err != nil {
		return "error: " + err.Error()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

func TestFailoverLifecycle(t *testing.T) {
	tn := newTestTailnet(t)
	hub := tn.add("hub", config.HoldHub)
	sb := tn.add("sb", config.HoldStandby)
	laptop := tn.add("laptop", config.HoldNever)

	tn.start(hub)
	tn.waitFor("the hub leads", tn.leaderIs("hub"))
	tn.start(sb)
	tn.start(laptop)
	writeLogin(t, hub, "acct.json", "t1")
	tn.waitFor("the standby gets the login", func() bool { return refreshToken(sb, "acct.json") == "t1" })

	for _, tm := range []*testMachine{hub, sb, laptop} {
		tn.waitFor(tm.name+"'s localhost reaches the hub", func() bool {
			return strings.Contains(get(tm.front, "/v1/models"), "on hub")
		})
	}

	writeLogin(t, hub, "acct.json", "t2")
	tn.waitFor("the refresh is pushed", func() bool { return refreshToken(sb, "acct.json") == "t2" })

	tn.net.setCutOff("hub", true)
	time.Sleep(2 * time.Second) // longer than FailoverAfter
	if l := tn.leaders(); len(l) != 1 || l[0] != "hub" {
		t.Fatalf("a broken path, with the hub still online, made leaders %v", l)
	}
	tn.net.setCutOff("hub", false)

	tn.crash(hub)
	tn.waitFor("the standby takes over", tn.leaderIs("sb"))
	if token := refreshToken(sb, "acct.json"); token != "t2" {
		t.Fatalf("the standby leads with token %q, want t2", token)
	}
	tn.waitFor("the laptop follows the new leader", func() bool {
		return strings.Contains(get(laptop.front, "/v1/models"), "on sb")
	})
	writeLogin(t, sb, "acct.json", "t3")

	tn.start(hub)
	tn.waitFor("the hub leads again after the hand-back", tn.leaderIs("hub"))
	if token := refreshToken(hub, "acct.json"); token != "t3" {
		t.Fatalf("the hub leads with token %q, want the standby's newer t3", token)
	}

	tn.waitFor("the standby is synced again", func() bool {
		r := sb.m.Report()
		return r.Synced != nil && r.Synced.Epoch == hub.m.Report().Epoch
	})
	stopped := time.Now()
	tn.shutDown(hub)
	tn.waitFor("the standby holds the accounts after a planned stop", tn.leaderIs("sb"))
	if waited := time.Since(stopped); waited > 900*time.Millisecond {
		t.Fatalf("a planned stop took %s: the accounts were dropped, not handed off", waited)
	}

	tn.shutDown(sb)
	tn.shutDown(laptop)
	if most := tn.most.Load(); most != 1 {
		t.Fatalf("%d proxies ran at once; never more than 1", most)
	}
}

func TestAHandOffWithNoAnswerLeavesNobodyRunningTwice(t *testing.T) {
	tn := newTestTailnet(t)
	hub := tn.add("hub", config.HoldHub)
	sb := tn.add("sb", config.HoldStandby)
	tn.start(hub)
	tn.waitFor("the hub leads", tn.leaderIs("hub"))
	tn.start(sb)
	tn.waitFor("the standby is synced", func() bool { return sb.m.Report().Synced != nil })
	tn.waitFor("the hub sees the standby", func() bool { return len(hub.m.peers.holders()) == 1 })

	tn.net.setCutOff("sb", true)
	if err := hub.m.handOff(context.Background(), "sb"); err == nil {
		t.Fatal("a hand-off nobody answered must not report success")
	}
	if l := tn.leaders(); len(l) != 0 {
		t.Fatalf("after an unanswered hand-off, leaders %v; the hub must stay stopped", l)
	}
	if leader := hub.m.Report().Leader; leader != "sb" {
		t.Fatalf("after an unanswered hand-off the hub presumes %q holds the accounts, want sb", leader)
	}
	tn.net.setCutOff("sb", false)
	tn.waitFor("one machine leads again", func() bool { return len(tn.leaders()) == 1 })

	tn.shutDown(hub)
	tn.shutDown(sb)
	if most := tn.most.Load(); most != 1 {
		t.Fatalf("%d proxies ran at once; never more than 1", most)
	}
}

func TestASlowTargetNeverRunsAlongsideASenderThatGaveUpWaiting(t *testing.T) {
	tn := newTestTailnet(t)
	hub := tn.add("hub", config.HoldHub)
	sb := tn.add("sb", config.HoldStandby)
	tn.start(hub)
	tn.waitFor("the hub leads", tn.leaderIs("hub"))
	tn.start(sb)
	tn.waitFor("the standby is synced", func() bool { return sb.m.Report().Synced != nil })
	tn.waitFor("the hub sees the standby", func() bool { return len(hub.m.peers.holders()) == 1 })

	release := sb.proxy.holdStarts()
	ctx, giveUp := context.WithTimeout(context.Background(), 300*time.Millisecond)
	handedAt := time.Now()
	_ = hub.m.handOff(ctx, "sb")
	giveUp()
	tn.waitFor("the hub sees the standby starting and follows it", func() bool {
		return hub.m.Report().Leader == "sb" && hubSees(hub, "sb", leadership.Starting)
	})
	tn.waitFor("several ticks after the request ended", func() bool { return time.Since(handedAt) > time.Second })
	if running := tn.running.Load(); running != 0 {
		t.Fatalf("%d proxies run while the target is still starting; the hub must stay stopped", running)
	}
	release()
	tn.waitFor("the standby holds the accounts", func() bool { return len(tn.leaders()) == 1 })

	tn.shutDown(hub)
	tn.shutDown(sb)
	if most := tn.most.Load(); most != 1 {
		t.Fatalf("%d proxies ran at once; never more than 1", most)
	}
}

func hubSees(tm *testMachine, peer string, state leadership.PeerState) bool {
	for _, p := range tm.m.Report().Peers {
		if p.Name == peer && p.State == state {
			return true
		}
	}
	return false
}

func TestTheKeyNeverGoesToADeviceWithoutSpinup(t *testing.T) {
	tn := newTestTailnet(t)
	var leaked, reached atomic.Bool
	var asked atomic.Int32
	imposter := func(answer string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get(api.KeyHeader) != "" || r.Header.Get("Authorization") != "" {
				leaked.Store(true)
			}
			if r.URL.Path == api.PathLeader {
				asked.Add(1)
			} else {
				reached.Store(true)
			}
			fmt.Fprint(w, answer)
		}))
	}
	stranger := imposter("CLI Proxy API Server")
	defer stranger.Close()
	spoofer := imposter(`{"name":"spoof","hold":"hub","leading":true,"epoch":99,"api_key_proof":"00","password_proof":"00"}`)
	defer spoofer.Close()
	tn.net.mu.Lock()
	tn.net.online["phone"], tn.net.peerAPI["phone"] = true, strings.TrimPrefix(stranger.URL, "http://")
	tn.net.online["spoof"], tn.net.peerAPI["spoof"] = true, strings.TrimPrefix(spoofer.URL, "http://")
	tn.net.mu.Unlock()

	hub := tn.add("hub", config.HoldHub)
	laptop := tn.add("laptop", config.HoldNever)
	tn.start(hub)
	tn.waitFor("the hub leads", tn.leaderIs("hub"))
	tn.start(laptop)
	tn.waitFor("the laptop reaches the hub, not the spoofer", func() bool {
		return strings.Contains(get(laptop.front, "/v1/models"), "on hub")
	})
	tn.shutDown(laptop)
	tn.shutDown(hub)
	if _, met := hub.m.ledger.members()["spoof"]; met {
		t.Error("a device that couldn't prove it knows the password became a member")
	}
	if asked.Load() < 4 {
		t.Fatal("the machines never looked at the imposters")
	}
	if leaked.Load() {
		t.Fatal("a key or API key went to a device that doesn't know the secrets")
	}
	if reached.Load() {
		t.Fatal("traffic was forwarded to a device that doesn't know the API key")
	}
}

func TestTakeoverIsRefusedWhileThisMachineLeads(t *testing.T) {
	tn := newTestTailnet(t)
	hub := tn.add("hub", config.HoldHub)
	tn.start(hub)
	tn.waitFor("the hub leads", tn.leaderIs("hub"))
	if got := post(hub.front, api.PathTakeover, "m"); got != http.StatusConflict {
		t.Errorf("takeover on the leader: %d, want 409", got)
	}
	tn.shutDown(hub)
}

func TestStoppingOverTheAPIHandsTheAccountsOver(t *testing.T) {
	tn := newTestTailnet(t)
	hub := tn.add("hub", config.HoldHub)
	sb := tn.add("sb", config.HoldStandby)
	tn.start(hub)
	tn.waitFor("the hub leads", tn.leaderIs("hub"))
	tn.start(sb)
	tn.waitFor("the standby is synced", func() bool { return sb.m.Report().Synced != nil })
	tn.waitFor("the hub sees the standby", func() bool { return len(hub.m.peers.holders()) == 1 })

	if got := post(hub.peerAPI, api.PathStop, "m"); got != http.StatusNotFound {
		t.Errorf("stop from the tailnet: %d, want 404", got)
	}
	if got := post(hub.front, api.PathStop, "wrong"); got != http.StatusUnauthorized {
		t.Errorf("stop with the wrong key: %d, want 401", got)
	}
	if got := post(hub.front, api.PathStop, "m"); got != http.StatusOK {
		t.Fatalf("stop: %d, want 200", got)
	}
	tn.waitFor("the service exits", func() bool {
		select {
		case <-hub.done:
			return true
		default:
			return false
		}
	})
	tn.waitFor("the standby holds the accounts", tn.leaderIs("sb"))
	tn.shutDown(hub)
	tn.shutDown(sb)
	if most := tn.most.Load(); most != 1 {
		t.Fatalf("%d proxies ran at once; never more than 1", most)
	}
}

func TestThePeerAPINeedsTheKey(t *testing.T) {
	tn := newTestTailnet(t)
	hub := tn.add("hub", config.HoldHub)
	tn.start(hub)
	tn.waitFor("the hub leads", tn.leaderIs("hub"))
	status := func(call, key string) int {
		method, path, _ := strings.Cut(call, " ")
		req, _ := http.NewRequest(method, "http://"+hub.peerAPI+path, strings.NewReader("{}"))
		req.Header.Set("X-Spinup-Key", key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for _, call := range []string{"GET /spinup/logins", "GET /spinup/secrets", "GET /spinup/state",
		"POST /spinup/logins", "POST /spinup/receive", "POST /spinup/handoff"} {
		if got := status(call, ""); got != http.StatusUnauthorized {
			t.Errorf("%s without the key: %d, want 401", call, got)
		}
	}
	if got := status("POST /spinup/takeover", "m"); got != http.StatusNotFound {
		t.Errorf("takeover from the tailnet, even with the key: %d, want 404", got)
	}
	if !strings.Contains(get(hub.peerAPI, "/spinup/leader"), `"leading":true`) {
		t.Error("/spinup/leader must be public")
	}
	tn.shutDown(hub)
}

func post(addr, path, key string) int {
	req, _ := http.NewRequest(http.MethodPost, "http://"+addr+path, strings.NewReader("{}"))
	req.Header.Set(api.KeyHeader, key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return -1
	}
	resp.Body.Close()
	return resp.StatusCode
}

func freeAddr(t *testing.T) string { return fmt.Sprintf("127.0.0.1:%d", freePort(t)) }

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

type testLog struct {
	t       *testing.T
	machine string
}

func (w testLog) Write(p []byte) (int, error) {
	w.t.Logf("[%s] %s", w.machine, strings.TrimSpace(string(p)))
	return len(p), nil
}
