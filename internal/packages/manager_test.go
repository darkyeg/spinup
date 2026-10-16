package packages

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darkyeg/spinup/internal/library"
)

type fakeResolver struct {
	data     []byte
	revision string
	calls    int
	fetches  int
}

func (f *fakeResolver) Resolve(context.Context, string, string) (string, error) {
	f.calls++
	return f.revision, nil
}
func (f *fakeResolver) Fetch(context.Context, string, string) ([]byte, error) {
	f.calls++
	f.fetches++
	return f.data, nil
}

type fakeNative struct {
	states   map[string]nativeState
	installs int
	failNext bool
	roots    map[string]string
}

func (f *fakeNative) Inspect(_ context.Context, t Target) (nativeState, error) {
	s, ok := f.states[t.Name]
	if !ok {
		s = nativeState{markets: map[string]string{}}
		f.states[t.Name] = s
	}
	return s, nil
}
func (f *fakeNative) Register(_ context.Context, t Target, root string) error {
	data, err := os.ReadFile(filepath.Join(root, ".claude-plugin", "marketplace.json"))
	if err != nil {
		return err
	}
	var catalog struct{ Name string }
	if err := json.Unmarshal(data, &catalog); err != nil {
		return err
	}
	s, _ := f.Inspect(context.Background(), t)
	s.markets[catalog.Name] = root
	f.states[t.Name] = s
	f.roots[t.Name] = root
	return nil
}
func (f *fakeNative) Install(_ context.Context, t Target, id string) error {
	f.installs++
	if f.failNext {
		f.failNext = false
		return errors.New("fixture activation failed")
	}
	s, _ := f.Inspect(context.Background(), t)
	_, market, _ := strings.Cut(id, "@")
	root := s.markets[market]
	data, err := os.ReadFile(filepath.Join(root, ".claude-plugin", "marketplace.json"))
	if err != nil {
		return err
	}
	var c struct {
		Plugins []struct{ Name, Source string }
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return err
	}
	data, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(c.Plugins[0].Source), ".claude-plugin", "plugin.json"))
	if err != nil {
		return err
	}
	var meta struct{ Name, Version string }
	if err := json.Unmarshal(data, &meta); err != nil {
		return err
	}
	_ = f.Remove(context.Background(), t, id)
	s = f.states[t.Name]
	s.plugins = append(s.plugins, installed{id, meta.Name, meta.Version, "user", true})
	f.states[t.Name] = s
	return nil
}
func (f *fakeNative) Remove(_ context.Context, t Target, id string) error {
	s := f.states[t.Name]
	var keep []installed
	for _, p := range s.plugins {
		if p.ID != id {
			keep = append(keep, p)
		}
	}
	s.plugins = keep
	f.states[t.Name] = s
	return nil
}
func (f *fakeNative) Forget(_ context.Context, t Target, name string) error {
	s := f.states[t.Name]
	delete(s.markets, name)
	return nil
}

func archiveFixture(t *testing.T, marker string) []byte {
	t.Helper()
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	files := map[string]string{
		".claude-plugin/plugin.json": `{"name":"probe","version":"1.0.0","description":"fixture"}`,
		".codex-plugin/plugin.json":  `{"name":"probe","version":"1.0.0","description":"fixture","skills":"./skills/","hooks":{}}`,
		"skills/probe/SKILL.md":      "---\nname: probe\ndescription: Inert fixture\n---\n" + marker,
		"scripts/helper.sh":          "#!/bin/sh\nprintf probe\n",
	}
	for name, data := range files {
		header := &zip.FileHeader{Name: "repo/" + name, Method: zip.Store}
		header.SetMode(0o644)
		if strings.HasSuffix(name, ".sh") {
			header.SetMode(0o755)
		}
		f, err := w.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func packageFixture(t *testing.T) (Manager, *fakeResolver, *fakeNative) {
	t.Helper()
	root := t.TempDir()
	m := New(library.At(filepath.Join(root, "library")), []Target{{"claude-code", filepath.Join(root, "claude")}, {"codex", filepath.Join(root, "codex")}})
	f := &fakeResolver{data: archiveFixture(t, "first"), revision: strings.Repeat("a", 40)}
	n := &fakeNative{states: map[string]nativeState{}, roots: map[string]string{}}
	m.resolve, m.native = f, n
	return m, f, n
}

func TestPackageLifecyclePinsAndRepairsWithoutRefetching(t *testing.T) {
	m, f, n := packageFixture(t)
	ctx := context.Background()
	p, err := m.Add(ctx, "https://github.com/owner/repo", "main", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if n.installs != 2 || f.calls != 2 {
		t.Fatalf("repeat sync installed/fetched again: %d/%d", n.installs, f.calls)
	}
	f.data = archiveFixture(t, "second")
	f.revision = strings.Repeat("b", 40)
	updated, err := m.Update(ctx, p.Source, "")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Previous == nil || updated.Previous.Digest != p.Release.Digest {
		t.Fatal("previous revision lost")
	}
	if _, err := m.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if n.installs != 4 {
		t.Fatal("same-version revision was skipped")
	}
	if err := m.Rollback(ctx, p.Source); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove(ctx, p.Source); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	for _, s := range n.states {
		if len(s.plugins) != 0 || len(s.markets) != 0 {
			t.Fatal("native registrations remain")
		}
	}
	if _, err := os.Stat(m.lib.Path(manifestFile)); err != nil {
		t.Fatal("empty declaration must survive removal for old-peer protection")
	}
}

func TestFailedReplacementRestoresPreviousAndKeepsTargetsIndependent(t *testing.T) {
	m, f, n := packageFixture(t)
	ctx := context.Background()
	p, err := m.Add(ctx, "owner/repo", "main", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	f.data = archiveFixture(t, "second")
	f.revision = strings.Repeat("b", 40)
	next, err := m.Update(ctx, p.Source, "")
	if err != nil {
		t.Fatal(err)
	}
	n.failNext = true
	if _, err := m.Sync(ctx); err == nil {
		t.Fatal("activation failure hidden")
	}
	a, err := m.readReceipts(m.targets[0])
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.readReceipts(m.targets[1])
	if err != nil {
		t.Fatal(err)
	}
	if a.Entries[p.Source].Current.Digest != p.Release.Digest || b.Entries[p.Source].Current.Digest != next.Release.Digest {
		t.Fatal("failed target changed another target's revision")
	}
	if n.roots["claude-code"] == n.roots["codex"] {
		t.Fatal("targets share a mutable catalog")
	}
	if _, err := m.Sync(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestExternalInstallationAndLocalChangesArePreserved(t *testing.T) {
	m, _, n := packageFixture(t)
	ctx := context.Background()
	p, err := m.Add(ctx, "owner/repo", "main", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	n.states["claude-code"] = nativeState{plugins: []installed{{"probe@other", "probe", "1", "user", true}}, markets: map[string]string{}}
	if _, err := m.Sync(ctx); err == nil {
		t.Fatal("external duplicate not detected")
	}
	if len(n.states["claude-code"].plugins) != 1 {
		t.Fatal("external install changed")
	}
	local := m.forTarget(m.targets[1])
	file := filepath.Join(local.content(p.Source, p.Release), "skills", "probe", "SKILL.md")
	if err := os.WriteFile(file, []byte("personal edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := n.installs
	if _, err := m.Sync(ctx); err == nil {
		t.Fatal("local edits not detected")
	}
	if n.installs != before {
		t.Fatal("overwrote edited install")
	}
}

func TestDisabledPluginIsNotReenabled(t *testing.T) {
	m, _, n := packageFixture(t)
	ctx := context.Background()
	if _, err := m.Add(ctx, "owner/repo", "main", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	s := n.states["codex"]
	s.plugins[0].Enabled = false
	n.states["codex"] = s
	before := n.installs
	results, err := m.Sync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n.installs != before || results[1].Action != Disabled {
		t.Fatal("native disabled choice lost")
	}
}

func TestInterruptedInstallUsesWriteAheadOwnership(t *testing.T) {
	m, _, n := packageFixture(t)
	ctx := context.Background()
	p, err := m.Add(ctx, "owner/repo", "main", "", []string{"codex"})
	if err != nil {
		t.Fatal(err)
	}
	n.failNext = true
	if _, err := m.Sync(ctx); err == nil {
		t.Fatal("expected failure")
	}
	r, err := m.readReceipts(m.targets[1])
	if err != nil {
		t.Fatal(err)
	}
	if r.Entries[p.Source].Pending == nil {
		t.Fatal("pending install not persisted")
	}
	if _, err := m.Sync(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveRejectsUnsafeEntriesAndPreservesHiddenFiles(t *testing.T) {
	p, err := unpack(archiveFixture(t, "probe"), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.file(".codex-plugin/plugin.json"); !ok {
		t.Fatal("hidden manifest lost")
	}
	for _, name := range []string{"repo/../outside", "repo/C:/escape", "repo/AUX.txt", "repo/a\\b", "repo/.git/config"} {
		var buf bytes.Buffer
		w := zip.NewWriter(&buf)
		f, _ := w.Create(name)
		_, _ = f.Write([]byte("x"))
		_ = w.Close()
		if _, err := unpack(buf.Bytes(), ""); err == nil {
			t.Errorf("accepted %s", name)
		}
	}
	dir := t.TempDir()
	if err := p.write(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "scripts", "helper.sh")); err != nil {
		t.Fatal(err)
	}
	if err := p.verify(dir); err == nil {
		t.Fatal("missing helper not detected")
	}
}

func TestUnknownManifestCannotBeOverwritten(t *testing.T) {
	m, _, _ := packageFixture(t)
	if err := os.MkdirAll(m.lib.Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.lib.Path(manifestFile), []byte(`{"version":2,"packages":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Add(context.Background(), "owner/repo", "main", "", nil); err == nil {
		t.Fatal("future manifest overwritten")
	}
}

func TestTwoRequestedSourcesCannotInstallTheSamePluginName(t *testing.T) {
	m, _, n := packageFixture(t)
	ctx := context.Background()
	if _, err := m.Add(ctx, "owner/first", "main", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Add(ctx, "owner/second", "main", "", nil); err != nil {
		t.Fatal(err)
	}
	results, err := m.Sync(ctx)
	if err == nil || n.installs != 0 {
		t.Fatalf("conflicting requests installed: %d %+v %v", n.installs, results, err)
	}
	for _, r := range results {
		if r.Action != Conflict {
			t.Fatalf("not flagged: %+v", r)
		}
	}
}
