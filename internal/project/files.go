package project

import (
	"encoding/json"
	"io/fs"
	"path"
	"strings"
)

const maxDepth = 3

var skippedDirs = map[string]bool{
	"node_modules": true, "dist": true, "build": true, "target": true,
	"vendor": true, "out": true, "bin": true, "obj": true,
}

func listFiles(repo fs.FS) []string {
	var files []string
	fs.WalkDir(repo, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fs.SkipDir
		}
		if !entry.IsDir() {
			files = append(files, name)
			return nil
		}
		if name != "." && skipsDir(name) {
			return fs.SkipDir
		}
		return nil
	})
	return files
}

func skipsDir(name string) bool {
	base := path.Base(name)
	return skippedDirs[base] || strings.HasPrefix(base, ".") || strings.Count(name, "/")+1 > maxDepth
}

func packageDeps(repo fs.FS, files []string) map[string]bool {
	deps := map[string]bool{}
	for _, file := range files {
		if path.Base(file) != "package.json" {
			continue
		}
		raw, err := fs.ReadFile(repo, file)
		if err != nil {
			continue
		}
		var manifest struct {
			Dependencies     map[string]json.RawMessage `json:"dependencies"`
			DevDependencies  map[string]json.RawMessage `json:"devDependencies"`
			PeerDependencies map[string]json.RawMessage `json:"peerDependencies"`
		}
		if json.Unmarshal(raw, &manifest) != nil {
			continue
		}
		for _, group := range []map[string]json.RawMessage{manifest.Dependencies, manifest.DevDependencies, manifest.PeerDependencies} {
			for name := range group {
				deps[name] = true
			}
		}
	}
	return deps
}
