package service

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"time"
)

// servePeers listens on the Tailscale address, and moves when Tailscale gives this machine a new one.
func (m *Machine) servePeers(ctx context.Context) {
	for ctx.Err() == nil {
		addr := m.peerListenAddr()
		if addr == "" {
			sleep(ctx, 2*time.Second)
			continue
		}
		l, err := net.Listen("tcp", addr)
		if err != nil {
			m.log.Printf("peer API: can't listen on %s: %v (retrying)", addr, err)
			sleep(ctx, 10*time.Second)
			continue
		}
		m.log.Printf("peer API on %s", addr)
		srv := &http.Server{Handler: m.routes(onTailnet), ReadHeaderTimeout: 30 * time.Second}
		served := make(chan struct{})
		go func() {
			defer close(served)
			_ = srv.Serve(l)
		}()
		m.closeWhenMoved(ctx, addr, served)
		srv.Close()
		<-served
	}
}

// closeWhenMoved returns when ctx ends, the server stops, or this machine's address changes.
func (m *Machine) closeWhenMoved(ctx context.Context, addr string, served <-chan struct{}) {
	check := time.NewTicker(10 * time.Second)
	defer check.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-served:
			return
		case <-check.C:
			if now := m.peerListenAddr(); now != "" && now != addr {
				return
			}
		}
	}
}

func (m *Machine) peerListenAddr() string {
	if m.o.PeerListen != "" {
		return m.o.PeerListen
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.self.IP == "" {
		return ""
	}
	return net.JoinHostPort(m.self.IP, strconv.Itoa(m.cfg.Port))
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
