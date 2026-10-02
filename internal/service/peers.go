package service

import (
	"context"
	"fmt"
	"sync"

	"github.com/darkyeg/spinup/internal/api"
)

// client speaks for this machine to other holders: it carries the key, the name and the hold.
func (m *Machine) client() api.Client {
	return api.Client{HTTP: m.httpClient, Key: m.o.Secrets.ManagementPassword, From: m.tail.selfName(), Hold: m.cfg.Hold}
}

// publicClient is for devices that haven't shown they are spinup holders: it carries no key.
func (m *Machine) publicClient() api.Client {
	return api.Client{HTTP: m.httpClient, From: m.tail.selfName()}
}

func (m *Machine) peerURL(name, ip, path string) string {
	return "http://" + m.o.PeerAddr(name, ip) + path
}

// getPeer and callPeer carry the key, so they reach only a holder that proved itself at the last look.
func (m *Machine) getPeer(ctx context.Context, name, path string, out any) error {
	url, err := m.provenURL(name, path)
	if err != nil {
		return err
	}
	return m.client().Get(ctx, url, out)
}

func (m *Machine) callPeer(ctx context.Context, name, path string, in, out any) error {
	url, err := m.provenURL(name, path)
	if err != nil {
		return err
	}
	return m.client().Post(ctx, url, in, out)
}

func (m *Machine) provenURL(name, path string) (string, error) {
	if _, proven := m.peers.reportOf(name); !proven {
		return "", fmt.Errorf("%s hasn't shown that it runs spinup", name)
	}
	return m.urlOf(name, path)
}

// askPeer is for questions that need no key.
func (m *Machine) askPeer(ctx context.Context, name, path string, out any) error {
	url, err := m.urlOf(name, path)
	if err != nil {
		return err
	}
	return m.publicClient().Get(ctx, url, out)
}

func (m *Machine) urlOf(name, path string) (string, error) {
	node, ok := m.tail.peer(name)
	if !ok {
		return "", fmt.Errorf("%s is not on the tailnet", name)
	}
	return m.peerURL(name, node.IP, path), nil
}

// eachPeer runs do for every peer at once, so one unreachable machine doesn't delay the rest.
func eachPeer(peers []string, do func(peer string)) {
	var wg sync.WaitGroup
	for _, p := range peers {
		wg.Go(func() { do(p) })
	}
	wg.Wait()
}
