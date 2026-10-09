package skills

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestStoreLockAcrossProcesses(t *testing.T) {
	if store := os.Getenv("SPINUP_TEST_LOCK_STORE"); store != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		lock, err := (paths{store: store}).lockStore(ctx)
		if lock != nil {
			lock.Close()
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("another process entered the locked store: %v", err)
		}
		return
	}
	p := paths{store: t.TempDir()}
	lock, err := p.lockStore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestStoreLockAcrossProcesses$")
	cmd.Env = append(os.Environ(), "SPINUP_TEST_LOCK_STORE="+p.store)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child lock check: %v\n%s", err, output)
	}
}

func TestSyncSerializesStoreWriters(t *testing.T) {
	f := newFixture(t, sampleManifest)
	var entered atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	f.manager.install = func(ctx context.Context, _ string, _ []string, names []string) (string, error) {
		if entered.Add(1) == 1 {
			close(started)
			<-release
		} else {
			<-ctx.Done()
			return "", ctx.Err()
		}
		for _, name := range names {
			write(t, filepath.Join(f.paths.store, name, skillFile), "---\ndescription: installed\n---\n")
		}
		return "", nil
	}
	first := make(chan error, 1)
	go func() { _, err := f.manager.Sync(context.Background()); first <- err }()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := f.manager.Sync(ctx)
	close(release)
	if firstErr := <-first; firstErr != nil {
		t.Fatal(firstErr)
	}
	if !errors.Is(err, context.DeadlineExceeded) || entered.Load() != 1 {
		t.Fatalf("overlapping sync entered the store: installers=%d, error=%v", entered.Load(), err)
	}
	if _, err := f.manager.Sync(context.Background()); err != nil {
		t.Fatalf("store stayed locked after sync: %v", err)
	}
}
