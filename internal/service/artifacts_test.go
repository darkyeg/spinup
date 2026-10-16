package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/filelock"
	"github.com/darkyeg/spinup/internal/library"
)

func artifactMachine(t *testing.T) *Machine {
	t.Helper()
	m := New(Options{Library: library.At(t.TempDir()), StatePath: filepath.Join(t.TempDir(), "state.json"), Secrets: config.Secrets{APIKey: "fixture-key"}, Log: log.New(io.Discard, "", 0)})
	m.openLibrary()
	return m
}

func requestArtifact(t *testing.T, m *Machine, digest string) {
	t.Helper()
	data := fmt.Sprintf(`{"version":1,"packages":[{"source":"owner/repo","ref":"main","release":{"revision":"%s","digest":"%s","name":"probe","version":"1","targets":["codex"]}}]}`, strings.Repeat("a", 40), digest)
	putFile(t, m.library.lib.Path(library.Packages), data)
}

func TestArtifactTransferIsSeparateAndOnlyOncePerMissingDigest(t *testing.T) {
	here, there := artifactMachine(t), artifactMachine(t)
	data := []byte("opaque zip payload")
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	requestArtifact(t, here, digest)
	requestArtifact(t, there, digest)
	if err := there.library.lib.PutArtifact(digest, data); err != nil {
		t.Fatal(err)
	}
	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == api.PathLibraryState:
			there.withAPIKey(there.serveLibraryState)(w, r)
		case strings.HasPrefix(r.URL.Path, api.PathLibraryArtifact):
			downloads.Add(1)
			there.withAPIKey(there.serveArtifact)(w, r)
		default:
			t.Errorf("unexpected whole-library request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	for range 3 {
		a, err := here.library.state(time.Now())
		if err != nil {
			t.Fatal(err)
		}
		b, err := there.library.state(time.Now())
		if err != nil {
			t.Fatal(err)
		}
		here.exchangeArtifacts(t.Context(), server.URL, a, b)
	}
	if downloads.Load() != 1 {
		t.Fatalf("downloaded %d times", downloads.Load())
	}
	got, err := here.library.lib.Artifact(digest)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("artifact %q %v", got, err)
	}
	files, err := here.library.lib.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasPrefix(f.Path, "artifacts/") || strings.HasPrefix(f.Path, "local/") {
			t.Fatal("machine state or artifacts embedded in snapshot")
		}
	}
	select {
	case <-here.library.installWanted:
	default:
		t.Fatal("arrival did not schedule installation")
	}
}

func TestArtifactRoutesRequireAuthenticationAndValidateContent(t *testing.T) {
	m := artifactMachine(t)
	data := []byte("package")
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	requestArtifact(t, m, digest)
	for _, test := range []struct {
		name, path, key string
		body            []byte
		want            int
	}{
		{"no key", digest, "", data, http.StatusUnauthorized},
		{"bad id", "../escape", "fixture-key", data, http.StatusBadRequest},
		{"wrong bytes", digest, "fixture-key", []byte("other"), http.StatusBadRequest},
		{"not requested", strings.Repeat("b", 64), "fixture-key", data, http.StatusConflict},
		{"valid", digest, "fixture-key", data, http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, api.PathLibraryArtifact+test.path, bytes.NewReader(test.body))
			r.Header.Set(api.APIKeyHeader, test.key)
			r.Header.Set(api.ForwardedHeader, test.name)
			w := httptest.NewRecorder()
			m.withAPIKey(m.acceptArtifact)(w, r)
			if w.Code != test.want {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
	m = artifactMachine(t)
	requestArtifact(t, m, digest)
	r := httptest.NewRequest(http.MethodPost, api.PathLibraryArtifact+digest, io.LimitReader(zeroReader{}, library.ArtifactLimit+1))
	r.Header.Set(api.APIKeyHeader, "fixture-key")
	w := httptest.NewRecorder()
	m.withAPIKey(m.acceptArtifact)(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: %d", w.Code)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestLegacyPeerCannotErasePackages(t *testing.T) {
	m := artifactMachine(t)
	requestArtifact(t, m, strings.Repeat("a", 64))
	now := time.Now()
	_, err := m.library.snapshot(now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	files := []library.File{{Path: library.Instructions, Data: []byte("old client edit")}}
	incoming := api.Library{Stamp: library.Stamp{Hash: library.Fingerprint(files), ChangedAt: now}, Files: files}
	if err := m.library.take(incoming, now); err == nil {
		t.Fatal("legacy update erased package intent")
	}
	if _, err := os.Stat(m.library.lib.Path(library.Packages)); err != nil {
		t.Fatal(err)
	}
	if err := m.library.lib.Take(files, now); err == nil {
		t.Fatal("direct replacement can erase package intent")
	}
}

func TestRejectedPackageOmissionDoesNotPoisonTracker(t *testing.T) {
	m := artifactMachine(t)
	requestArtifact(t, m, strings.Repeat("a", 64))
	before, err := m.library.snapshot(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	files := []library.File{{Path: library.Instructions, Data: []byte("incoming")}}
	incoming := api.Library{Protocol: 2, Files: files, Stamp: library.Stamp{Hash: library.Fingerprint(files), ChangedAt: time.Now()}}
	if err := m.library.take(incoming, time.Now()); err == nil {
		t.Fatal("omission accepted")
	}
	after, err := m.library.snapshot(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if after.Stamp != before.Stamp {
		t.Fatalf("rejection changed tracker: %+v -> %+v", before.Stamp, after.Stamp)
	}
}

func TestTakeComparesAfterConcurrentPackageEdit(t *testing.T) {
	m := artifactMachine(t)
	requestArtifact(t, m, strings.Repeat("a", 64))
	before, err := m.library.snapshot(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	incoming := before
	incoming.Files = append(incoming.Files, library.File{Path: library.Instructions, Data: []byte("incoming")})
	incoming.Stamp = library.Stamp{Hash: library.Fingerprint(incoming.Files), ChangedAt: time.Now()}
	lock, err := filelock.Acquire(context.Background(), m.library.lib.Path(".packages.lock"))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- m.library.take(incoming, time.Now()) }()
	time.Sleep(50 * time.Millisecond)
	requestArtifact(t, m, strings.Repeat("b", 64))
	_ = lock.Close()
	select {
	case err := <-done:
		if err != errNotNewer {
			t.Fatalf("concurrent edit not protected: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("take blocked")
	}
	data, err := os.ReadFile(m.library.lib.Path(library.Packages))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), strings.Repeat("b", 64)) {
		t.Fatal("local edit overwritten")
	}
}

func TestHealthyMemberRepairsCorruptHubArtifact(t *testing.T) {
	member, hub := artifactMachine(t), artifactMachine(t)
	data := []byte("healthy")
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	requestArtifact(t, member, digest)
	requestArtifact(t, hub, digest)
	if err := member.library.lib.PutArtifact(digest, data); err != nil {
		t.Fatal(err)
	}
	putFile(t, hub.library.lib.Path("artifacts/"+digest+".zip"), "corrupt")
	a, _ := member.library.state(time.Now())
	b, _ := hub.library.state(time.Now())
	if len(b.Artifacts) != 0 {
		t.Fatal("corrupt content advertised as usable")
	}
	server := httptest.NewServer(http.HandlerFunc(hub.withAPIKey(hub.acceptArtifact)))
	defer server.Close()
	member.exchangeArtifacts(t.Context(), server.URL, a, b)
	got, err := hub.library.lib.Artifact(digest)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("corrupt hub was not repaired: %q %v", got, err)
	}
}

func TestNewPeerDoesNotSendPackagesToLegacyPeer(t *testing.T) {
	m := artifactMachine(t)
	requestArtifact(t, m, strings.Repeat("a", 64))
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == api.PathLibraryState {
			_ = json.NewEncoder(w).Encode(api.LibraryState{})
			return
		}
		writes.Add(1)
		writeJSON(w, okBody)
	}))
	defer server.Close()
	m.compareLibrary(t.Context(), server.URL)
	m.compareLibrary(t.Context(), server.URL)
	if writes.Load() != 0 {
		t.Fatal("sent package-bearing library to legacy peer")
	}
}
