package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/logins"
)

const mergeIdleWithin = 30 * time.Second

// mergeLogins serializes imports with ownership changes and the proxy's credential writer.
func (m *Machine) mergeLogins(ctx context.Context, incoming api.Logins) (logins.Merged, error) {
	m.transition.Lock()
	defer m.transition.Unlock()
	if err := ctx.Err(); err != nil {
		return logins.Merged{}, err
	}
	fromLeader := m.fromLeader(incoming)
	offered := fromLeader && m.replica.offeredTo(incoming.From, incoming.Epoch)
	extent := logins.Some
	if offered {
		extent = extentOf(incoming)
	}
	var merged logins.Merged
	var err error
	if m.ledger.view(time.Now()).Leading {
		merged, err = m.mergeWhileLeading(ctx, incoming.Files)
	} else {
		merged, err = m.mergeStopped(incoming.Files, extent)
	}
	if err != nil {
		return merged, err
	}
	if fromLeader {
		if merged.Changed() {
			m.log.Printf("synced from %s: updated %v, removed %v", incoming.From, merged.Written, merged.Removed)
		}
		if offered {
			m.replica.merged(incoming.Epoch, extent == logins.Everything)
		} else {
			m.replica.pullSoon()
		}
	}
	return merged, nil
}

// mergeStopped needs m.transition held and a stopped proxy.
func (m *Machine) mergeStopped(files []logins.File, extent logins.Extent) (logins.Merged, error) {
	if m.o.Proxy != nil && m.o.Proxy.Running() {
		return logins.Merged{}, errors.New("cannot merge logins while the proxy is running")
	}
	return logins.Merge(m.cfg.AuthDir, m.cfg.RemovedDir(), files, extent)
}

// mergeWhileLeading needs m.transition held; a busy or fenced machine defers the import.
func (m *Machine) mergeWhileLeading(ctx context.Context, files []logins.File) (logins.Merged, error) {
	needed, err := logins.NeedsMerge(m.cfg.AuthDir, files)
	if err != nil || !needed {
		return logins.Merged{}, err
	}
	if m.activity.inFlight() > 0 {
		m.log.Printf("waiting for running requests before importing logins")
	}
	wait, cancel := context.WithTimeout(ctx, mergeIdleWithin)
	err = m.activity.pause(wait)
	cancel()
	if err != nil {
		return logins.Merged{}, err
	}
	if err := ctx.Err(); err != nil {
		m.activity.resume()
		return logins.Merged{}, err
	}
	m.o.Proxy.Stop(m.quiet)
	if m.o.Proxy.Running() {
		m.activity.resume()
		return logins.Merged{}, errors.New("proxy did not stop; deferred login merge")
	}
	if m.activity.mustStop() {
		m.ledger.stopLeading()
		m.save()
		return logins.Merged{}, errMustStop
	}
	merged, mergeErr := m.mergeStopped(files, logins.Some)
	if err := m.resumeAfterMerge(); err != nil {
		return merged, errors.Join(mergeErr, err)
	}
	if merged.Changed() {
		m.replica.pushAgain()
	}
	return merged, mergeErr
}

// Once the proxy is stopped, a canceled caller must not prevent recovery. Fencing still cancels it.
func (m *Machine) resumeAfterMerge() error {
	ctx, cancel := context.WithTimeout(context.Background(), proxyStartTimeout)
	defer cancel()
	stopping := m.activity.stopRequested()
	select {
	case <-stopping:
		cancel()
	case <-m.serviceDone:
		cancel()
	default:
	}
	if err := ctx.Err(); err != nil {
		m.ledger.stopLeading()
		m.save()
		return err
	}
	go func() {
		select {
		case <-stopping:
			cancel()
		case <-m.serviceDone:
			cancel()
		case <-ctx.Done():
		}
	}()
	err := m.launchProxy(ctx)
	if err == nil {
		select {
		case <-m.serviceDone:
			err = context.Canceled
		default:
			err = ctx.Err()
		}
	}
	if err == nil && m.activity.resume() {
		return nil
	}
	m.o.Proxy.Stop(nil)
	m.ledger.stopLeading()
	m.save()
	if err == nil {
		err = errMustStop
	}
	return fmt.Errorf("resume proxy after login merge: %w", err)
}
