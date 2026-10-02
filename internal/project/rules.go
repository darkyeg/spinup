package project

import (
	"encoding/json"
	"io/fs"
	"path"
	"slices"
	"strings"
)

const rulesFile = "skills/per-repo.json"

type rule struct {
	Stack    string              `json:"stack"`
	Files    []string            `json:"files"`
	Packages []string            `json:"packages"`
	Contains []string            `json:"contains"`
	Skills   map[string][]string `json:"skills"`
	Note     string              `json:"note"`
}

func loadRules(data fs.FS) ([]rule, error) {
	raw, err := fs.ReadFile(data, rulesFile)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Rules []rule `json:"rules"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	return parsed.Rules, nil
}

func (r rule) matches(repo fs.FS, files []string, deps map[string]bool) bool {
	hits := r.hits(files)
	byPackage := slices.ContainsFunc(r.Packages, func(p string) bool { return deps[p] })
	if len(hits) == 0 && !byPackage {
		return false
	}
	if r.Contains == nil {
		return true
	}
	return slices.ContainsFunc(hits, func(file string) bool { return fileContainsAny(repo, file, r.Contains) })
}

func (r rule) hits(files []string) []string {
	var hits []string
	for _, file := range files {
		name := path.Base(file)
		if slices.ContainsFunc(r.Files, func(pattern string) bool { ok, _ := path.Match(pattern, name); return ok }) {
			hits = append(hits, file)
		}
	}
	return hits
}

func fileContainsAny(repo fs.FS, file string, needles []string) bool {
	text, err := fs.ReadFile(repo, file)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(needles, func(needle string) bool { return strings.Contains(string(text), needle) })
}
