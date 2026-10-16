package packages

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/darkyeg/spinup/internal/atomicfile"
	"github.com/darkyeg/spinup/internal/library"
)

const manifestFile = "packages.json"

type Release struct {
	Revision string   `json:"revision"`
	Digest   string   `json:"digest"`
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Path     string   `json:"path,omitempty"`
	Targets  []string `json:"targets"`
}

type Package struct {
	Source   string   `json:"source"`
	Ref      string   `json:"ref"`
	Release  Release  `json:"release"`
	Previous *Release `json:"previous,omitempty"`
}

type manifest struct {
	Version  int       `json:"version"`
	Packages []Package `json:"packages"`
}

var sourcePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$`)
var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,99}$`)
var revisionPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

func sourceID(source string) (string, error) {
	source = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(source), "/"), ".git")
	source = strings.TrimPrefix(source, "https://github.com/")
	if !sourcePattern.MatchString(source) {
		return "", errors.New("use a GitHub owner/repo or https://github.com/owner/repo URL")
	}
	return strings.ToLower(source), nil
}

func readManifest(lib library.Library) (manifest, error) {
	m := manifest{Version: 1, Packages: []Package{}}
	data, err := os.ReadFile(lib.Path(manifestFile))
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	return parseManifest(data)
}

func ValidateManifest(data []byte) error { _, err := parseManifest(data); return err }

func parseManifest(data []byte) (manifest, error) {
	m := manifest{}
	if len(data) > 1<<20 {
		return m, errors.New("package list exceeds 1 MiB")
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("packages.json: %w", err)
	}
	if m.Version != 1 {
		return m, fmt.Errorf("package format %d is unsupported; update spinup", m.Version)
	}
	seen := map[string]bool{}
	for _, p := range m.Packages {
		id, err := sourceID(p.Source)
		if err != nil || id != p.Source || seen[id] {
			return m, fmt.Errorf("invalid or repeated package source %q", p.Source)
		}
		seen[id] = true
		if p.Ref == "" {
			return m, fmt.Errorf("%s: missing ref", id)
		}
		if err := validateRelease(p.Release); err != nil {
			return m, fmt.Errorf("%s: %w", id, err)
		}
		if p.Previous != nil {
			if err := validateRelease(*p.Previous); err != nil {
				return m, err
			}
		}
	}
	return m, nil
}

func validateRelease(r Release) error {
	if !revisionPattern.MatchString(r.Revision) || !library.ValidDigest(r.Digest) || !namePattern.MatchString(r.Name) || r.Version == "" {
		return errors.New("invalid locked package identity")
	}
	if r.Path != "" && !safeRelative(r.Path) {
		return errors.New("invalid package subdirectory")
	}
	if len(r.Targets) == 0 {
		return errors.New("package has no targets")
	}
	seen := map[string]bool{}
	for _, t := range r.Targets {
		if (t != "claude-code" && t != "codex") || seen[t] {
			return fmt.Errorf("unsupported or repeated target %q", t)
		}
		seen[t] = true
	}
	return nil
}

func writeManifest(lib library.Library, m manifest) error {
	slices.SortFunc(m.Packages, func(a, b Package) int { return strings.Compare(a.Source, b.Source) })
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(lib.Path(manifestFile), append(data, '\n'), 0o600)
}

func RequiredArtifacts(lib library.Library) ([]string, error) {
	m, err := readManifest(lib)
	if err != nil {
		return nil, err
	}
	var digests []string
	for _, p := range m.Packages {
		digests = append(digests, p.Release.Digest)
		if p.Previous != nil {
			digests = append(digests, p.Previous.Digest)
		}
	}
	slices.Sort(digests)
	return slices.Compact(digests), nil
}
