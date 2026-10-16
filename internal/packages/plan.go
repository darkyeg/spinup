package packages

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
)

type Action string

const (
	Ready       Action = "ready"
	Install     Action = "install"
	Replace     Action = "replace"
	Uninstall   Action = "remove"
	Conflict    Action = "conflict"
	Unavailable Action = "unavailable"
	Disabled    Action = "disabled"
)

type Result struct {
	Source  string `json:"source"`
	Target  string `json:"target"`
	Version string `json:"version,omitempty"`
	Action  Action `json:"action"`
	Detail  string `json:"detail,omitempty"`
}

func (m Manager) Plan(ctx context.Context) ([]Result, error) {
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
		receipts, err := local.readReceipts(t)
		if err != nil {
			return results, err
		}
		wanted := wantedFor(list, t)
		if len(wanted) == 0 && len(receipts.Entries) == 0 {
			continue
		}
		observed, err := local.native.Inspect(ctx, t)
		if err != nil {
			results = append(results, Result{Target: t.Name, Action: Unavailable, Detail: err.Error()})
			continue
		}
		results = append(results, local.planTarget(t, wanted, receipts, observed)...)
	}
	return results, nil
}

func wantedFor(list manifest, t Target) map[string]Package {
	wanted := map[string]Package{}
	for _, p := range list.Packages {
		if contains(p.Release.Targets, t.Name) {
			wanted[p.Source] = p
		}
	}
	return wanted
}

func (m Manager) planTarget(t Target, wanted map[string]Package, receipts receipts, state nativeState) []Result {
	keys := map[string]bool{}
	for k := range wanted {
		keys[k] = true
	}
	for k := range receipts.Entries {
		keys[k] = true
	}
	var sources []string
	for k := range keys {
		sources = append(sources, k)
	}
	slices.Sort(sources)
	var results []Result
	for _, source := range sources {
		p, desired := wanted[source]
		owned, ours := receipts.Entries[source]
		r := p.Release
		if !desired {
			if owned.Pending != nil {
				r = *owned.Pending
			} else {
				r = *owned.Current
			}
		}
		result := Result{Source: source, Target: t.Name, Version: r.Version, Action: Install}
		if desired {
			for other, requested := range wanted {
				if other != source && requested.Release.Name == r.Name {
					result.Action = Conflict
					result.Detail = "requested packages share plugin name " + r.Name + ": " + other
					break
				}
			}
			if result.Action == Conflict {
				results = append(results, result)
				continue
			}
		}
		market := marketplace(source)
		id := r.Name + "@" + market
		if existing, ok := state.markets[market]; ok && (!ours || !samePath(existing, m.root(source))) {
			result.Action = Conflict
			result.Detail = "marketplace already exists outside this package's ownership"
			results = append(results, result)
			continue
		}
		var active *installed
		for _, plugin := range state.plugins {
			if plugin.ID == id {
				copy := plugin
				active = &copy
			}
			if desired && plugin.Name == r.Name && plugin.ID != id {
				result.Action = Conflict
				result.Detail = "another installation already provides " + r.Name + ": " + plugin.ID
			}
		}
		if active != nil && (!ours || active.Scope != "user") {
			result.Action = Conflict
			result.Detail = "plugin is not owned by Spinup at user scope"
		}
		if active != nil && ours && owned.Pending == nil && owned.Current != nil && active.Version != owned.Current.Version {
			result.Action = Conflict
			result.Detail = "native plugin version changed outside Spinup"
		}
		if result.Action == Conflict {
			results = append(results, result)
			continue
		}
		if !desired {
			result.Action = Uninstall
			results = append(results, result)
			continue
		}
		if active != nil && !active.Enabled {
			result.Action = Disabled
			result.Detail = "disabled in the native tool; enable it there before syncing updates"
			results = append(results, result)
			continue
		}
		if err := m.checkContent(source, r); err != nil {
			result.Action = Conflict
			result.Detail = err.Error()
			results = append(results, result)
			continue
		}
		if owned.Current != nil {
			if err := m.checkContent(source, *owned.Current); err != nil {
				result.Action = Conflict
				result.Detail = "installed revision: " + err.Error()
				results = append(results, result)
				continue
			}
		}
		if active != nil {
			result.Action = Replace
			_, contentErr := os.Stat(m.content(source, r))
			if owned.Current != nil && owned.Pending == nil && owned.Current.Digest == r.Digest && contentErr == nil && active.Version == r.Version {
				result.Action = Ready
			}
		}
		if owned.Pending != nil {
			result.Detail = "resume interrupted package installation"
		}
		results = append(results, result)
	}
	return results
}

func pendingError(results []Result) error {
	var pending []string
	for _, r := range results {
		if r.Action == Conflict || r.Action == Unavailable {
			pending = append(pending, fmt.Sprintf("%s %s: %s", r.Source, r.Target, r.Detail))
		}
	}
	if len(pending) > 0 {
		return fmt.Errorf("packages need attention: %s", strings.Join(pending, "; "))
	}
	return nil
}
