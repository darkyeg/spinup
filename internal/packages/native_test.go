package packages

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darkyeg/spinup/internal/library"
)

func TestNativePluginLifecycle(t *testing.T) {
	if os.Getenv("SPINUP_TEST_NATIVE_PLUGINS") != "1" {
		t.Skip("opt-in isolated native CLI smoke test")
	}
	for _, name := range []string{"claude-code", "codex"} {
		t.Run(name, func(t *testing.T) {
			bin := name
			if name == "claude-code" {
				bin = "claude"
			}
			if _, err := exec.LookPath(bin); err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			home := filepath.Join(root, "tool")
			if err := os.MkdirAll(home, 0o700); err != nil {
				t.Fatal(err)
			}
			m := New(library.At(filepath.Join(root, "library")), []Target{{name, home}})
			f := &fakeResolver{data: archiveFixture(t, "first"), revision: strings.Repeat("a", 40)}
			m.resolve = f
			ctx := context.Background()
			p, err := m.Add(ctx, "fixture/native-plugin", "main", "", []string{name})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = m.Remove(ctx, p.Source); _, _ = m.Sync(ctx) })
			if _, err := m.Sync(ctx); err != nil {
				t.Fatal(err)
			}
			if results, err := m.Plan(ctx); err != nil || len(results) != 1 || results[0].Action != Ready {
				t.Fatalf("plan: %+v %v", results, err)
			}
			f.data = archiveFixture(t, "second")
			f.revision = strings.Repeat("b", 40)
			if _, err := m.Update(ctx, p.Source, ""); err != nil {
				t.Fatal(err)
			}
			if _, err := m.Sync(ctx); err != nil {
				t.Fatal(err)
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
			state, err := m.native.Inspect(ctx, m.targets[0])
			if err != nil {
				t.Fatal(err)
			}
			if len(state.plugins) != 0 || len(state.markets) != 0 {
				t.Fatalf("temporary registrations remain: %+v", state)
			}
		})
	}
}

func TestSuperpowersNativePackage(t *testing.T) {
	if os.Getenv("SPINUP_TEST_SUPERPOWERS") != "1" {
		t.Skip("opt-in network and isolated native plugin smoke test")
	}
	root := t.TempDir()
	targets := []Target{{"claude-code", filepath.Join(root, "claude")}, {"codex", filepath.Join(root, "codex")}}
	for _, target := range targets {
		if err := os.MkdirAll(target.Home, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	m := New(library.At(filepath.Join(root, "library")), targets)
	ctx := context.Background()
	p, err := m.Add(ctx, "obra/superpowers", "bb92a77741419a4ab5f06e711a283343f1ada0c3", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Remove(ctx, p.Source); _, _ = m.Sync(ctx) })
	if results, err := m.Sync(ctx); err != nil {
		t.Fatalf("%+v: %v", results, err)
	}
	results, err := m.Plan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("missing target: %+v", results)
	}
	for _, r := range results {
		if r.Action != Ready {
			t.Fatalf("not installed: %+v", r)
		}
	}
	if err := m.Remove(ctx, p.Source); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Sync(ctx); err != nil {
		t.Fatal(err)
	}
}
