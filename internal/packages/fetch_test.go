package packages

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"testing"
)

func TestUpdateReusesLockedArchivesAndRejectsChangedBytes(t *testing.T) {
	m, f, _ := packageFixture(t)
	p, err := m.Add(t.Context(), "owner/repo", "main", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	firstData := f.data
	info, err := os.Stat(m.lib.Path(manifestFile))
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if _, err := m.Update(t.Context(), p.Source, ""); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := os.Stat(m.lib.Path(manifestFile))
	if f.fetches != 1 || !after.ModTime().Equal(info.ModTime()) {
		t.Fatalf("unchanged update downloaded %d archives or rewrote shared manifest", f.fetches)
	}
	if _, err := m.Update(t.Context(), p.Source, "stable"); err != nil {
		t.Fatal(err)
	}
	list, err := m.List()
	if err != nil || list[0].Ref != "stable" || f.fetches != 1 {
		t.Fatalf("ref-only update: %+v %v", list, err)
	}
	f.revision = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	f.data = archiveFixture(t, "second")
	if _, err := m.Update(t.Context(), p.Source, ""); err != nil {
		t.Fatal(err)
	}
	f.revision, f.data = p.Release.Revision, firstData
	if _, err := m.Update(t.Context(), p.Source, ""); err != nil {
		t.Fatal(err)
	}
	if f.fetches != 2 {
		t.Fatal("previous revision was downloaded again")
	}
	if err := os.Remove(m.lib.Path("artifacts/" + p.Release.Digest + ".zip")); err != nil {
		t.Fatal(err)
	}
	f.data = archiveFixture(t, "unexpected upstream bytes")
	before, err := os.ReadFile(m.lib.Path(manifestFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Update(t.Context(), p.Source, ""); err == nil {
		t.Fatal("changed bytes silently replaced lock")
	}
	unchanged, _ := os.ReadFile(m.lib.Path(manifestFile))
	if !bytes.Equal(before, unchanged) {
		t.Fatal("failed repair changed manifest")
	}
	f.data = firstData
	if _, err := m.Update(t.Context(), p.Source, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := m.lib.Artifact(p.Release.Digest); err != nil {
		t.Fatal(err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGitHubResolveDoesNotDownloadArchive(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	requests := 0
	g := github{client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Host != "api.github.com" {
			t.Fatal("resolution downloaded archive")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(`{"sha":"` + sha + `"}`)), Header: http.Header{}}, nil
	})}}
	for _, ref := range []string{"main", sha} {
		got, err := g.Resolve(context.Background(), "owner/repo", ref)
		if err != nil || got != sha {
			t.Fatalf("resolve: %q %v", got, err)
		}
	}
	if requests != 1 {
		t.Fatal("pinned commit made an unnecessary request")
	}
}
