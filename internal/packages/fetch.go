package packages

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/darkyeg/spinup/internal/library"
)

type resolver interface {
	Resolve(context.Context, string, string) (string, error)
	Fetch(context.Context, string, string) ([]byte, error)
}

type github struct{ client *http.Client }

func (g github) get(ctx context.Context, address string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "spinup")
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub returned %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download exceeds %d MiB", limit>>20)
	}
	return data, nil
}

func (g github) Resolve(ctx context.Context, source, ref string) (string, error) {
	if revisionPattern.MatchString(ref) {
		return ref, nil
	}
	data, err := g.get(ctx, "https://api.github.com/repos/"+source+"/commits/"+url.PathEscape(ref), 1<<20)
	if err != nil {
		return "", err
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(data, &commit); err != nil {
		return "", err
	}
	if !revisionPattern.MatchString(commit.SHA) {
		return "", fmt.Errorf("GitHub did not return a commit SHA")
	}
	return commit.SHA, nil
}

func (m Manager) archive(ctx context.Context, source, revision string, known ...Release) ([]byte, error) {
	lockedDigest := ""
	for _, r := range known {
		if r.Revision == revision {
			lockedDigest = r.Digest
			if data, err := m.lib.Artifact(r.Digest); err == nil {
				return data, nil
			}
			break
		}
	}
	data, err := m.resolve.Fetch(ctx, source, revision)
	if err == nil && lockedDigest != "" && digest(data) != lockedDigest {
		return nil, fmt.Errorf("upstream bytes differ from the locked package; restore its artifact from a healthy peer")
	}
	return data, err
}

func (g github) Fetch(ctx context.Context, source, revision string) ([]byte, error) {
	return g.get(ctx, "https://codeload.github.com/"+source+"/zip/"+revision, library.ArtifactLimit)
}

func defaultResolver() resolver { return github{&http.Client{Timeout: 2 * time.Minute}} }
func digest(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }
