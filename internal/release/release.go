// Package release downloads files from a GitHub project's latest release, checked against its checksums.txt.
package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type Release struct {
	Version string
	assets  map[string]string
}

// Latest is the newest release of repo ("owner/name").
func Latest(ctx context.Context, repo string) (Release, error) {
	var raw struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	data, err := get(ctx, "https://api.github.com/repos/"+repo+"/releases/latest")
	if err != nil {
		return Release{}, err
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Release{}, fmt.Errorf("%s releases: %w", repo, err)
	}
	rel := Release{Version: strings.TrimPrefix(raw.TagName, "v"), assets: map[string]string{}}
	for _, a := range raw.Assets {
		rel.assets[a.Name] = a.URL
	}
	return rel, nil
}

// Fetch downloads one file of the release and refuses it unless it matches checksums.txt.
func (r Release) Fetch(ctx context.Context, name string) ([]byte, error) {
	url, ok := r.assets[name]
	if !ok {
		return nil, fmt.Errorf("release %s has no %s", r.Version, name)
	}
	sumsURL, ok := r.assets["checksums.txt"]
	if !ok {
		return nil, fmt.Errorf("release %s has no checksums.txt", r.Version)
	}
	data, err := get(ctx, url)
	if err != nil {
		return nil, err
	}
	sums, err := get(ctx, sumsURL)
	if err != nil {
		return nil, fmt.Errorf("checksums.txt: %w", err)
	}
	sum := sha256.Sum256(data)
	if want := checksumFor(string(sums), name); want == "" || want != hex.EncodeToString(sum[:]) {
		return nil, fmt.Errorf("checksum mismatch for %s: refusing it", name)
	}
	return data, nil
}

func checksumFor(sums, name string) string {
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			return fields[0]
		}
	}
	return ""
}

func get(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "spinup")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" && strings.HasPrefix(url, "https://api.github.com/") {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}
