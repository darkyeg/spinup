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

const latestReleaseAPI = "https://api.github.com/repos/router-for-me/CLIProxyAPI/releases/latest"

type Release struct {
	Version string
	Assets  map[string]string // file name to download URL
}

func Installed(c config.Config) bool {
	_, err := os.Stat(c.ProxyExe())
	return err == nil
}

func LatestRelease(ctx context.Context) (Release, error) {
	var raw struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	data, err := download(ctx, latestReleaseAPI)
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

// Install puts rel's binary in the proxy folder after checking it against the release's
// checksums.txt. The old binary is renamed aside, since Windows can't overwrite a running one.
func Install(ctx context.Context, c config.Config, rel Release) error {
	archive, err := assetName(rel.Version)
	if err != nil {
		return err
	}
	url, ok := rel.Assets[archive]
	if !ok {
		return fmt.Errorf("release %s has no %s", rel.Version, archive)
	}
	data, err := download(ctx, url)
	if err != nil {
		return err
	}
	sums, err := download(ctx, rel.Assets["checksums.txt"])
	if err != nil {
		return fmt.Errorf("checksums.txt: %w", err)
	}
	sum := sha256.Sum256(data)
	if checksumFor(string(sums), archive) != hex.EncodeToString(sum[:]) {
		return fmt.Errorf("checksum mismatch for %s: refusing to install", archive)
	}
	exeName := filepath.Base(c.ProxyExe())
	files, err := extract(archive, data, map[string]bool{exeName: true, "config.example.yaml": true, "README.md": true, "LICENSE": true})
	if err != nil {
		return err
	}
	if files[exeName] == nil {
		return fmt.Errorf("%s is missing from %s", exeName, archive)
	}
	if err := os.MkdirAll(c.ProxyDir, 0o700); err != nil {
		return err
	}
	for name, content := range files {
		if name == exeName {
			continue
		}
		if err := os.WriteFile(filepath.Join(c.ProxyDir, name), content, 0o644); err != nil {
			return err
		}
	}
	return replaceExe(c.ProxyExe(), files[exeName])
}

func replaceExe(exe string, content []byte) error {
	if err := os.WriteFile(exe+".new", content, 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(exe); err == nil {
		if err := os.Rename(exe, fmt.Sprintf("%s.old%d", exe, time.Now().Unix())); err != nil {
			return err
		}
	}
	return os.Rename(exe+".new", exe)
}

func assetName(version string) (string, error) {
	arch := map[string]string{"amd64": "amd64", "arm64": "aarch64"}[runtime.GOARCH]
	if arch == "" {
		return "", fmt.Errorf("CLIProxyAPI has no build for %s", runtime.GOARCH)
	}
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("CLIProxyAPI_%s_%s_%s.%s", version, runtime.GOOS, arch, ext), nil
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

func extract(archive string, data []byte, keep map[string]bool) (map[string][]byte, error) {
	if strings.HasSuffix(archive, ".zip") {
		return extractZip(data, keep)
	}
	return extractTarGz(data, keep)
}

func extractZip(data []byte, keep map[string]bool) (map[string][]byte, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	for _, f := range z.File {
		name := path.Base(f.Name)
		if !keep[name] || f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		files[name], err = io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

func extractTarGz(data []byte, keep map[string]bool) (map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return files, nil
		}
		if err != nil {
			return nil, err
		}
		if name := path.Base(h.Name); keep[name] && h.Typeflag == tar.TypeReg {
			if files[name], err = io.ReadAll(tr); err != nil {
				return nil, err
			}
		}
	}
}

func download(ctx context.Context, url string) ([]byte, error) {
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
