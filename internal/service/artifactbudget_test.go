package service

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/darkyeg/spinup/internal/api"
)

func TestArtifactBudgetSurvivesRestartAndStopsFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transfers.json")
	logger := log.New(io.Discard, "", 0)
	now := time.Now()
	for attempt := range 3 {
		b := openArtifactBudget(path, logger)
		b.now = func() time.Time { return now }
		if !b.begin("get", false) {
			t.Fatalf("attempt %d refused", attempt)
		}
		if b.begin("get", false) {
			t.Fatal("immediate retry allowed")
		}
		now = now.Add(5 * time.Minute)
	}
	b := openArtifactBudget(path, logger)
	b.now = func() time.Time { return now.Add(365 * 24 * time.Hour) }
	if b.begin("get", false) {
		t.Fatal("exhausted budget revived with time or restart")
	}
	b.healthy("get")
	if !b.begin("get", false) {
		t.Fatal("new loss after observed recovery was not repairable")
	}
	b.succeeded("get")
	b = openArtifactBudget(path, logger)
	if b.begin("get", false) {
		t.Fatal("successful transfer repeated after restart")
	}
}

func TestArtifactBudgetFailsClosedWithOneDiagnostic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transfers.json")
	if err := os.WriteFile(path, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	b := openArtifactBudget(path, log.New(&logs, "", 0))
	for range 100 {
		if b.begin("get", false) {
			t.Fatal("lost transfer memory permits downloads")
		}
	}
	if bytes.Count(logs.Bytes(), []byte("\n")) != 1 {
		t.Fatalf("repeated diagnostics: %s", &logs)
	}
	path = filepath.Join(t.TempDir(), "missing", "transfers.json")
	b = openArtifactBudget(path, log.New(&logs, "", 0))
	if err := os.WriteFile(filepath.Dir(path), []byte("block directory creation"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b.begin("get", false) || b.begin("get", false) {
		t.Fatal("transfer started without durable reservation")
	}
}

func TestArtifactTrafficIsBoundedForUnchangedFailure(t *testing.T) {
	for _, direction := range []string{"get", "put"} {
		for _, outcome := range []string{"failure", "success"} {
			t.Run(direction+"/"+outcome, func(t *testing.T) {
				m := artifactMachine(t)
				data := []byte("bounded artifact payload")
				digest := fmt.Sprintf("%x", sha256.Sum256(data))
				requestArtifact(t, m, digest)
				a := api.LibraryState{Protocol: 2, Required: []string{digest}}
				b := a
				if direction == "put" {
					if err := m.library.lib.PutArtifact(digest, data); err != nil {
						t.Fatal(err)
					}
					a.Artifacts = []string{digest}
				} else {
					b.Artifacts = []string{digest}
				}
				requests, transferred := 0, 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests++
					if direction == "put" {
						n, _ := io.Copy(io.Discard, r.Body)
						transferred += int(n)
						if outcome == "failure" {
							w.WriteHeader(http.StatusInternalServerError)
						}
						return
					}
					payload := data
					if outcome == "failure" {
						payload = bytes.Repeat([]byte("x"), len(data))
					}
					n, _ := w.Write(payload)
					transferred += n
				}))
				defer server.Close()
				now := time.Now()
				for range 500 {
					m.library.artifacts.now = func() time.Time { return now }
					m.exchangeArtifacts(t.Context(), server.URL, a, b)
					if direction == "get" && outcome == "success" {
						// A filesystem cleaner removes the successful download before
						// the next round can observe a healthy destination.
						_ = os.Remove(m.library.lib.Path("artifacts/" + digest + ".zip"))
					}
					now = now.Add(15 * time.Second)
					m.library.artifacts = openArtifactBudget(m.library.artifacts.path, m.log)
				}
				want := 3
				if outcome == "success" {
					want = 1
				}
				if requests != want || transferred != want*len(data) {
					t.Fatalf("requests=%d bytes=%d; want %d requests, %d bytes", requests, transferred, want, want*len(data))
				}
			})
		}
	}
}

func TestArtifactTransientFailureAndLaterLossAreRepairable(t *testing.T) {
	for _, direction := range []string{"get", "put"} {
		t.Run(direction, func(t *testing.T) {
			sender, receiver := artifactMachine(t), artifactMachine(t)
			data := []byte("repairable content")
			digest := fmt.Sprintf("%x", sha256.Sum256(data))
			requestArtifact(t, sender, digest)
			requestArtifact(t, receiver, digest)
			if err := sender.library.lib.PutArtifact(digest, data); err != nil {
				t.Fatal(err)
			}
			initiator, remote := receiver, sender
			if direction == "put" {
				initiator, remote = sender, receiver
			}
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if requests == 1 {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				if direction == "get" {
					remote.serveArtifact(w, r)
				} else {
					remote.acceptArtifact(w, r)
				}
			}))
			defer server.Close()
			now := time.Now()
			initiator.library.artifacts.now = func() time.Time { return now }
			remote.library.artifacts.now = func() time.Time { return now }
			exchange := func() {
				a, err := initiator.library.state(now)
				if err != nil {
					t.Fatal(err)
				}
				b, err := remote.library.state(now)
				if err != nil {
					t.Fatal(err)
				}
				initiator.exchangeArtifacts(t.Context(), server.URL, a, b)
			}
			exchange()
			exchange()
			if requests != 1 {
				t.Fatal("transient failure caused immediate retry")
			}
			now = now.Add(time.Minute)
			exchange()
			if _, err := receiver.library.lib.Artifact(digest); err != nil {
				t.Fatalf("transient failure not repaired: %v", err)
			}
			for range 10 {
				exchange()
			}
			if requests != 2 {
				t.Fatalf("healthy content retransmitted: %d requests", requests)
			}
			putFile(t, receiver.library.lib.Path("artifacts/"+digest+".zip"), "corrupted later")
			now = now.Add(time.Minute)
			exchange()
			if _, err := receiver.library.lib.Artifact(digest); err != nil || requests != 3 {
				t.Fatalf("later loss not repaired: requests=%d error=%v", requests, err)
			}
		})
	}
}

type countedArtifactReader struct{ read int }

func (r *countedArtifactReader) Read(p []byte) (int, error) {
	r.read += len(p)
	return len(p), nil
}

func TestArtifactUploadRefusedBeforeReadingBody(t *testing.T) {
	m := artifactMachine(t)
	data := []byte("valid content")
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	requestArtifact(t, m, digest)
	w := httptest.NewRecorder()
	m.acceptArtifact(w, httptest.NewRequest(http.MethodPost, api.PathLibraryArtifact+digest, bytes.NewReader([]byte("corrupt"))))
	if w.Code != http.StatusBadRequest {
		t.Fatal(w.Code)
	}
	body := &countedArtifactReader{}
	w = httptest.NewRecorder()
	m.acceptArtifact(w, httptest.NewRequest(http.MethodPost, api.PathLibraryArtifact+digest, body))
	if w.Code != http.StatusTooManyRequests || body.read != 0 {
		t.Fatalf("throttled upload: status=%d bytes=%d", w.Code, body.read)
	}
	if err := m.library.lib.PutArtifact(digest, data); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	m.acceptArtifact(w, httptest.NewRequest(http.MethodPost, api.PathLibraryArtifact+digest, body))
	if w.Code != http.StatusOK || body.read != 0 {
		t.Fatalf("already present upload: status=%d bytes=%d", w.Code, body.read)
	}
}

func TestArtifactServerBoundsRepeatedDownloadsAcrossRestarts(t *testing.T) {
	m := artifactMachine(t)
	data := []byte("cached payload")
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	if err := m.library.lib.PutArtifact(digest, data); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	transferred := 0
	for range 1000 {
		m.library.artifacts.now = func() time.Time { return now }
		r := httptest.NewRequest(http.MethodGet, api.PathLibraryArtifact+digest, nil)
		w := httptest.NewRecorder()
		m.serveArtifact(w, r)
		if w.Code == http.StatusOK {
			transferred += w.Body.Len()
		} else if w.Code != http.StatusTooManyRequests {
			t.Fatalf("unexpected status %d", w.Code)
		}
		now = now.Add(15 * time.Second)
		m.library.artifacts = openArtifactBudget(m.library.artifacts.path, m.log)
	}
	if transferred != 3*len(data) {
		t.Fatalf("served %d bytes", transferred)
	}
	now = now.Add(24 * time.Hour)
	m.library.artifacts.now = func() time.Time { return now }
	w := httptest.NewRecorder()
	m.serveArtifact(w, httptest.NewRequest(http.MethodGet, api.PathLibraryArtifact+digest, nil))
	if w.Code != http.StatusOK {
		t.Fatal("server did not renew its daily budget")
	}
}
