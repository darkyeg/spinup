package shell

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// AsRoot runs the program directly; Windows steps that need admin go through Elevated.
func AsRoot(ctx context.Context, name string, args ...string) error {
	return Run(ctx, name, args...)
}

// RefreshPath re-reads PATH from the registry, so programs installed during this run are found.
func RefreshPath() {
	paths := []string{os.Getenv("PATH")}
	for _, k := range []struct {
		root registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`},
		{registry.CURRENT_USER, `Environment`},
	} {
		key, err := registry.OpenKey(k.root, k.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		if v, _, err := key.GetStringValue("Path"); err == nil {
			if expanded, err := registry.ExpandString(v); err == nil {
				v = expanded
			}
			paths = append(paths, v)
		}
		key.Close()
	}
	os.Setenv("PATH", mergeWindowsPaths(paths...))
}

func mergeWindowsPaths(paths ...string) string {
	seen := map[string]bool{}
	var dirs []string
	for _, path := range paths {
		// An unmatched quote makes Go treat all subsequent directories as one.
		if strings.Count(path, `"`)%2 != 0 {
			path = repairWindowsPathQuotes(path)
		}
		for _, dir := range filepath.SplitList(path) {
			key := strings.ToLower(filepath.Clean(dir))
			if dir == "" || seen[key] {
				continue
			}
			seen[key] = true
			if strings.ContainsRune(dir, ';') {
				dir = `"` + dir + `"`
			}
			dirs = append(dirs, dir)
		}
	}
	return strings.Join(dirs, ";")
}

func repairWindowsPathQuotes(path string) string {
	var repaired strings.Builder
	for i := 0; i < len(path); i++ {
		if path[i] != '"' {
			repaired.WriteByte(path[i])
			continue
		}
		if i != 0 && path[i-1] != ';' {
			continue
		}
		closing := strings.IndexByte(path[i+1:], '"')
		if closing < 0 {
			continue
		}
		closing += i + 1
		if closing+1 == len(path) || path[closing+1] == ';' {
			repaired.WriteString(path[i : closing+1])
			i = closing
		}
	}
	return repaired.String()
}
