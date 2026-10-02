package proxy

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/darkyeg/spinup/internal/config"
)

const releasesAPI = "https://api.github.com/repos/router-for-me/CLIProxyAPI/releases/latest"

// Release is a CLIProxyAPI release.
type Release struct {
	Version string
	Assets  map[string]string // name -> download URL
}

// LatestRelease asks GitHub for the newest release.
func LatestRelease(ctx context.Context) (Release, error) {
	var raw struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	data, err := get(ctx, releasesAPI)
	if err != nil {
		return Release{}, err
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Release{}, err
	}
	rel := Release{Version: strings.TrimPrefix(raw.TagName, "v"), Assets: map[string]string{}}
	for _, a := range raw.Assets {
		rel.Assets[a.Name] = a.URL
	}
	return rel, nil
}

// AssetName is the archive for this OS and CPU.
func AssetName(version string) (string, error) {
	arch := map[string]string{"amd64": "amd64", "arm64": "aarch64"}[runtime.GOARCH]
	if arch == "" {
		return "", fmt.Errorf("unsupported CPU %s", runtime.GOARCH)
	}
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("CLIProxyAPI_%s_%s_%s.%s", version, runtime.GOOS, arch, ext), nil
}

// Install downloads rel, checks it against the release's checksums.txt, and puts the binary in
// the proxy folder. A running binary is renamed out of the way (Windows can't overwrite it).
func Install(ctx context.Context, c config.Config, rel Release) error {
	name, err := AssetName(rel.Version)
	if err != nil {
		return err
	}
	url, ok := rel.Assets[name]
	if !ok {
		return fmt.Errorf("release %s has no %s", rel.Version, name)
	}
	data, err := get(ctx, url)
	if err != nil {
		return err
	}
	sums, err := get(ctx, rel.Assets["checksums.txt"])
	if err != nil {
		return fmt.Errorf("checksums.txt: %w", err)
	}
	sum := sha256.Sum256(data)
	if want := checksumFor(string(sums), name); want != hex.EncodeToString(sum[:]) {
		return fmt.Errorf("checksum mismatch for %s: refusing to install", name)
	}
	exeName := filepath.Base(c.ProxyExe())
	keep := map[string]bool{exeName: true, "config.example.yaml": true, "README.md": true, "LICENSE": true}
	files, err := extract(name, data, keep)
	if err != nil {
		return err
	}
	if files[exeName] == nil {
		return fmt.Errorf("%s missing from %s", exeName, name)
	}
	if err := os.MkdirAll(c.ProxyDir, 0o700); err != nil {
		return err
	}
	for fname, content := range files {
		if fname != exeName {
			if err := os.WriteFile(filepath.Join(c.ProxyDir, fname), content, 0o644); err != nil {
				return err
			}
		}
	}
	exe := c.ProxyExe()
	if err := os.WriteFile(exe+".new", files[exeName], 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(exe); err == nil {
		if err := os.Rename(exe, fmt.Sprintf("%s.old%d", exe, time.Now().Unix())); err != nil {
			return err
		}
	}
	return os.Rename(exe+".new", exe)
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

func extract(name string, data []byte, keep map[string]bool) (map[string][]byte, error) {
	files := map[string][]byte{}
	if strings.HasSuffix(name, ".zip") {
		z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err
		}
		for _, f := range z.File {
			if base := path.Base(f.Name); keep[base] && !f.FileInfo().IsDir() {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				files[base], err = io.ReadAll(rc)
				rc.Close()
				if err != nil {
					return nil, err
				}
			}
		}
		return files, nil
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	t := tar.NewReader(gz)
	for {
		h, err := t.Next()
		if err == io.EOF {
			return files, nil
		}
		if err != nil {
			return nil, err
		}
		if base := path.Base(h.Name); keep[base] && h.Typeflag == tar.TypeReg {
			if files[base], err = io.ReadAll(t); err != nil {
				return nil, err
			}
		}
	}
}

func get(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "spinup")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" && strings.HasPrefix(url, "https://api.github.com/") {
		req.Header.Set("Authorization", "Bearer "+tok)
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
