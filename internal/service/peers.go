package service

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/darkyeg/spinup/internal/api"
)

// client speaks for this machine to other holders: it carries the key, the name and the hold.
func (m *Machine) client() api.Client {
	return api.Client{HTTP: m.httpClient, Key: m.o.Secrets.ManagementPassword, From: m.selfName(), Hold: m.cfg.Hold}
}

// publicClient is for devices that haven't shown they are spinup holders: it carries no key.
func (m *Machine) publicClient() api.Client {
	return api.Client{HTTP: m.httpClient, From: m.selfName()}
}

func (m *Machine) peerURL(name, ip, path string) string {
	return "http://" + m.o.PeerAddr(name, ip) + path
}

func (m *Machine) getPeer(ctx context.Context, name, path string, out any) error {
	url, err := m.urlOf(name, path)
	if err != nil {
		return err
	}
	return m.client().Get(ctx, url, out)
}

func (m *Machine) callPeer(ctx context.Context, name, path string, in, out any) error {
	url, err := m.urlOf(name, path)
	if err != nil {
		return err
	}
	return m.client().Post(ctx, url, in, out)
}

func (m *Machine) urlOf(name, path string) (string, error) {
	m.mu.Lock()
	node, ok := m.tailnet.Peer(name)
	m.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("%s is not on the tailnet", name)
	}
	return m.peerURL(name, node.IP, path), nil
}

// answeringMembers are the machines that can hold and answered the last look.
func (m *Machine) answeringMembers() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var names []string
	for name, r := range m.peers.answered {
		if r.Hold.CanHold() {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// eachPeer runs do for every peer at once, so one unreachable machine doesn't delay the rest.
func eachPeer(peers []string, do func(peer string)) {
	var wg sync.WaitGroup
	for _, p := range peers {
		wg.Go(func() { do(p) })
	}
	wg.Wait()
}
