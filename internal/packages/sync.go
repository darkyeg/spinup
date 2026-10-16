package packages

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/darkyeg/spinup/internal/filelock"
)

const recoveryTimeout = 2 * time.Minute

func (m Manager) Sync(ctx context.Context) ([]Result, error) {
	lock, err := m.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	list, err := readManifest(m.lib)
	if err != nil {
		return nil, err
	}
	var results []Result
	for _, t := range m.targets {
		local := m.forTarget(t)
		r, err := local.syncTarget(ctx, t, list)
		if err != nil {
			r = append(r, Result{Target: t.Name, Action: Unavailable, Detail: err.Error()})
		}
		results = append(results, r...)
	}
	return results, pendingError(results)
}

func (m Manager) syncTarget(ctx context.Context, t Target, list manifest) ([]Result, error) {
	receipts, err := m.readReceipts(t)
	if err != nil {
		return nil, err
	}
	wanted := wantedFor(list, t)
	if len(wanted) == 0 && len(receipts.Entries) == 0 {
		return nil, nil
	}
	state, err := m.native.Inspect(ctx, t)
	if err != nil {
		return nil, err
	}
	lock, err := filelock.Acquire(ctx, filepath.Join(t.Home, ".spinup-packages.lock"))
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	state, err = m.native.Inspect(ctx, t)
	if err != nil {
		return nil, err
	}
	results := m.planTarget(t, wanted, receipts, state)
	for i, result := range results {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		switch result.Action {
		case Install, Replace:
			p := wanted[result.Source]
			err = m.activate(ctx, t, p, receipts, state)
			if err == nil {
				results[i].Action = Ready
				results[i].Detail = "installed; restart the tool to load this revision"
			}
		case Uninstall:
			err = m.uninstall(ctx, t, result.Source, receipts, state)
			if err == nil {
				results[i].Detail = "removed native registration; cached revisions retained"
			}
		default:
			continue
		}
		if err != nil {
			results[i].Action = Conflict
			results[i].Detail = err.Error()
		}
		state, err = m.native.Inspect(ctx, t)
		if err != nil {
			return results, err
		}
	}
	return results, nil
}

func (m Manager) activate(ctx context.Context, t Target, p Package, receipts receipts, state nativeState) error {
	if err := m.materialize(p.Source, p.Release); err != nil {
		return err
	}
	old := receipts.Entries[p.Source]
	entry := old
	entry.Source = p.Source
	entry.Pending = &p.Release
	receipts.Entries[p.Source] = entry
	if err := m.writeReceipts(t, receipts); err != nil {
		return err
	}
	if err := m.installRevision(ctx, t, p.Source, p.Release, state); err != nil {
		if old.Current == nil {
			return err
		}
		recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recoveryTimeout)
		defer cancel()
		observed, inspectErr := m.native.Inspect(recoveryCtx, t)
		if inspectErr != nil {
			return errors.Join(err, fmt.Errorf("could not inspect for recovery: %w", inspectErr))
		}
		if recoveryErr := m.installRevision(recoveryCtx, t, p.Source, *old.Current, observed); recoveryErr != nil {
			return errors.Join(err, fmt.Errorf("previous revision recovery failed; run packages sync: %w", recoveryErr))
		}
		old.Pending = nil
		receipts.Entries[p.Source] = old
		return errors.Join(fmt.Errorf("%w; previous revision restored", err), m.writeReceipts(t, receipts))
	}
	receipts.Entries[p.Source] = receipt{Source: p.Source, Current: &p.Release}
	return m.writeReceipts(t, receipts)
}

func (m Manager) installRevision(ctx context.Context, t Target, source string, r Release, state nativeState) error {
	if err := m.materialize(source, r); err != nil {
		return err
	}
	if err := m.catalog(source, r); err != nil {
		return err
	}
	market := marketplace(source)
	id := r.Name + "@" + market
	if _, ok := state.markets[market]; !ok {
		if err := m.native.Register(ctx, t, m.root(source)); err != nil {
			return err
		}
	}
	if t.Name == "claude-code" && hasPlugin(state, id) {
		if err := m.native.Remove(ctx, t, id); err != nil {
			return err
		}
	}
	if err := m.native.Install(ctx, t, id); err != nil {
		return err
	}
	observed, err := m.native.Inspect(ctx, t)
	if err != nil {
		return err
	}
	for _, p := range observed.plugins {
		if p.ID == id && p.Version == r.Version && p.Enabled && p.Scope == "user" {
			return nil
		}
	}
	return errors.New("native tool did not report the requested plugin enabled at the expected version")
}

func (m Manager) uninstall(ctx context.Context, t Target, source string, receipts receipts, state nativeState) error {
	entry := receipts.Entries[source]
	r := entry.Current
	if entry.Pending != nil {
		r = entry.Pending
	}
	id := r.Name + "@" + marketplace(source)
	if hasPlugin(state, id) {
		if err := m.native.Remove(ctx, t, id); err != nil {
			return err
		}
	}
	if _, ok := state.markets[marketplace(source)]; ok {
		if err := m.native.Forget(ctx, t, marketplace(source)); err != nil {
			return err
		}
	}
	observed, err := m.native.Inspect(ctx, t)
	if err != nil {
		return err
	}
	if hasPlugin(observed, id) {
		return errors.New("native tool still reports the plugin after removal")
	}
	if _, ok := observed.markets[marketplace(source)]; ok {
		return errors.New("native tool still reports the marketplace after removal")
	}
	delete(receipts.Entries, source)
	return m.writeReceipts(t, receipts)
}

func hasPlugin(state nativeState, id string) bool {
	for _, p := range state.plugins {
		if p.ID == id {
			return true
		}
	}
	return false
}
