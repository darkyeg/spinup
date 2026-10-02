package cluster

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darkyeg/spinup/internal/authsync"
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/tailnet"
)

// ---- fakes

type fakeNet struct {
	mu      sync.Mutex
	online  map[string]bool
	seen    map[string]time.Time
	addr    map[string]string // peer API address as others reach it
	blocked map[string]bool   // online, but nobody can reach it (a broken path)
}

func (f *fakeNet) set(name string, online bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !online && f.online[name] {
		f.seen[name] = time.Now()
	}
	f.online[name] = online
}

type fakeSource struct {
	net  *fakeNet
	self string
}

func (s fakeSource) Status(context.Context) (tailnet.Status, error) {
	s.net.mu.Lock()
	defer s.net.mu.Unlock()
	st := tailnet.Status{Running: true, Self: tailnet.Node{Name: s.self, IP: "127.0.0.1", Online: true}}
	for name, on := range s.net.online {
		if name != s.self {
			st.Peers = append(st.Peers, tailnet.Node{Name: name, IP: "127.0.0.1", Online: on, LastSeen: s.net.seen[name]})
		}
	}
	return st, nil
}

// fakeProxy stands in for CLIProxyAPI: it answers on its port with its machine's name, and counts
// how many proxies run at once across the whole test.
type fakeProxy struct {
	name    string
	port    int
	running *int32
	maxRun  *int32
	mu      sync.Mutex
	srv     *http.Server
}

func (p *fakeProxy) Start(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.srv != nil {
		return nil
	}
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p.port))
	if err != nil {
		return err
	}
	p.srv = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "CLI Proxy API Server on %s", p.name)
	})}
	go p.srv.Serve(l)
	now := atomic.AddInt32(p.running, 1)
	for {
		m := atomic.LoadInt32(p.maxRun)
		if now <= m || atomic.CompareAndSwapInt32(p.maxRun, m, now) {
			break
		}
	}
	return nil
}

func (p *fakeProxy) Stop(func() bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.srv != nil {
		p.srv.Close()
		p.srv = nil
		atomic.AddInt32(p.running, -1)
	}
}

func (p *fakeProxy) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.srv != nil
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// ---- harness

type machine struct {
	name   string
	role   config.Role
	dir    string
	front  string
	peer   string
	node   *Node
	cancel context.CancelFunc
	done   chan struct{}
	proxy  *fakeProxy
}

type cluster struct {
	t       *testing.T
	net     *fakeNet
	running int32
	maxRun  int32
	secrets config.Secrets
	ms      map[string]*machine
}

func newCluster(t *testing.T) *cluster {
	return &cluster{t: t, secrets: config.Secrets{APIKey: "k", ManagementPassword: "m"},
		net: &fakeNet{online: map[string]bool{}, seen: map[string]time.Time{}, addr: map[string]string{},
			blocked: map[string]bool{}},
		ms: map[string]*machine{}}
}

func (c *cluster) add(name string, role config.Role) *machine {
	m := &machine{name: name, role: role, dir: c.t.TempDir(),
		front: fmt.Sprintf("127.0.0.1:%d", freePort(c.t)), peer: fmt.Sprintf("127.0.0.1:%d", freePort(c.t))}
	c.ms[name] = m
	c.net.mu.Lock()
	c.net.addr[name] = m.peer
	c.net.mu.Unlock()
	return m
}

func (c *cluster) start(m *machine) {
	cfg := config.Defaults()
	cfg.Role, cfg.ProxyPort, cfg.FailoverAfterSeconds, cfg.AutoFailback = m.role, freePort(c.t), 1, true
	cfg.AuthDir = filepath.Join(m.dir, "auth")
	m.proxy = &fakeProxy{name: m.name, port: cfg.ProxyPort, running: &c.running, maxRun: &c.maxRun}
	var runner Runner
	if m.role.Eligible() {
		runner = m.proxy
	}
	o := Options{
		Config: cfg, Tailnet: fakeSource{c.net, m.name}, Runner: runner, Version: "test",
		Log:       log.New(testWriter{c.t, m.name}, "", 0),
		StatePath: filepath.Join(m.dir, "state.json"), FrontListen: m.front, PeerListen: m.peer,
		Tick: 50 * time.Millisecond,
		PeerAddr: func(name, _ string) string {
			c.net.mu.Lock()
			defer c.net.mu.Unlock()
			if c.net.blocked[name] {
				return "127.0.0.1:1" // nothing listens there
			}
			return c.net.addr[name]
		},
	}
	if m.role.Eligible() {
		o.Secrets = c.secrets
	}
	m.node = New(o)
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel, m.done = cancel, make(chan struct{})
	c.net.set(m.name, true)
	go func() {
		defer close(m.done)
		if err := m.node.Run(ctx); err != nil {
			c.t.Errorf("%s: %v", m.name, err)
		}
	}()
}

// crash stops a machine without any hand-off, like a power cut.
func (c *cluster) crash(m *machine) {
	m.node.mu.Lock()
	m.node.isLeader = false // so shutdown doesn't hand off
	m.node.mu.Unlock()
	m.proxy.Stop(nil)
	m.cancel()
	<-m.done
	c.net.set(m.name, false)
}

func (c *cluster) stop(m *machine) {
	m.cancel()
	<-m.done
	c.net.set(m.name, false)
}

func (c *cluster) leaders() []string {
	var out []string
	for name, m := range c.ms {
		if m.node != nil && m.node.Report().IsLeader {
			out = append(out, name)
		}
	}
	return out
}

func (c *cluster) waitFor(what string, ok func() bool) {
	c.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	c.t.Fatalf("timed out waiting for: %s (leaders: %v)", what, c.leaders())
}

func (c *cluster) leaderIs(name string) func() bool {
	return func() bool { l := c.leaders(); return len(l) == 1 && l[0] == name }
}

type testWriter struct {
	t    *testing.T
	name string
}

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Logf("[%s] %s", w.name, strings.TrimSpace(string(p)))
	return len(p), nil
}

func writeLogin(t *testing.T, m *machine, name, token string, refreshed time.Time) {
	t.Helper()
	data := fmt.Sprintf(`{"type":"codex","refresh_token":%q,"last_refresh":%q}`, token, refreshed.UTC().Format(time.RFC3339Nano))
	if err := os.MkdirAll(m.node.cfg.AuthDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteFileAtomic(filepath.Join(m.node.cfg.AuthDir, name), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func token(m *machine, name string) string {
	data, _ := os.ReadFile(filepath.Join(m.node.cfg.AuthDir, name))
	return authsync.RefreshToken(data)
}

func get(t *testing.T, addr, path string) string {
	t.Helper()
	resp, err := http.Get("http://" + addr + path)
	if err != nil {
		return "error: " + err.Error()
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

// ---- the scenario

func TestFailoverLifecycle(t *testing.T) {
	c := newCluster(t)
	hub := c.add("hub", config.RoleHub)
	sb := c.add("sb", config.RoleStandby)
	cl := c.add("laptop", config.RoleClient)

	// 1. The hub starts first and holds the accounts; the standby follows; the client finds it.
	c.start(hub)
	c.waitFor("hub leads", c.leaderIs("hub"))
	c.start(sb)
	c.start(cl)
	writeLogin(t, hub, "acct.json", "t1", time.Now())
	c.waitFor("standby gets the login", func() bool { return token(sb, "acct.json") == "t1" })

	// 2. Every machine reaches the accounts through its own localhost front.
	for _, m := range []*machine{hub, sb, cl} {
		m := m
		c.waitFor(m.name+" front forwards to the hub", func() bool {
			return strings.Contains(get(t, m.front, "/v1/models"), "on hub")
		})
	}

	// 3. A refresh on the leader reaches the standby within moments.
	writeLogin(t, hub, "acct.json", "t2", time.Now())
	c.waitFor("refresh pushed", func() bool { return token(sb, "acct.json") == "t2" })

	// 4. A broken path to the hub (still online per Tailscale) must not make the standby lead.
	c.net.mu.Lock()
	c.net.blocked["hub"] = true
	c.net.mu.Unlock()
	time.Sleep(2 * time.Second) // longer than FailoverAfter
	if l := c.leaders(); len(l) != 1 || l[0] != "hub" {
		t.Fatalf("partition created leaders %v", l)
	}
	c.net.mu.Lock()
	c.net.blocked["hub"] = false
	c.net.mu.Unlock()

	// 5. The hub dies suddenly: after FailoverAfter the standby takes over with the synced login.
	c.crash(hub)
	c.waitFor("standby takes over", c.leaderIs("sb"))
	if tok := token(sb, "acct.json"); tok != "t2" {
		t.Fatalf("standby leads with token %q, want t2", tok)
	}
	c.waitFor("client follows the new leader", func() bool {
		return strings.Contains(get(t, cl.front, "/v1/models"), "on sb")
	})
	writeLogin(t, sb, "acct.json", "t3", time.Now()) // the standby refreshes while it leads

	// 6. The hub comes back: it must not use its old token; it syncs, then gets the accounts back.
	c.start(hub)
	c.waitFor("hub leads again after hand-back", c.leaderIs("hub"))
	if tok := token(hub, "acct.json"); tok != "t3" {
		t.Fatalf("hub leads with token %q, want the standby's newer t3", tok)
	}

	// 7. A planned stop hands the accounts over instead of dropping them.
	c.waitFor("standby synced again", func() bool {
		r := sb.node.Report()
		return r.SyncedSecondsAgo >= 0 && r.SyncedEpoch == hub.node.Report().Epoch
	})
	stopped := time.Now()
	c.stop(hub)
	c.waitFor("standby holds the accounts after a planned stop", c.leaderIs("sb"))
	if waited := time.Since(stopped); waited > 900*time.Millisecond {
		t.Fatalf("planned stop took %s: the accounts were not handed off (failover kicked in instead)", waited)
	}

	c.stop(sb)
	c.stop(cl)
	if max := atomic.LoadInt32(&c.maxRun); max != 1 {
		t.Fatalf("%d proxies ran at the same time; must never exceed 1", max)
	}
}

func TestPeerAPINeedsTheKey(t *testing.T) {
	c := newCluster(t)
	hub := c.add("hub", config.RoleHub)
	c.start(hub)
	c.waitFor("hub leads", c.leaderIs("hub"))
	for _, path := range []string{"/spinup/auth", "/spinup/secrets", "/spinup/state"} {
		resp, err := http.Get("http://" + hub.peer + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without the key: %d, want 401", path, resp.StatusCode)
		}
	}
	if !strings.Contains(get(t, hub.peer, "/spinup/leader"), `"is_leader":true`) {
		t.Error("/spinup/leader must be public")
	}
	c.stop(hub)
}
