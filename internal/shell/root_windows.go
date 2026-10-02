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
	parts := []string{os.Getenv("PATH")}
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
			parts = append(parts, v)
		}
		key.Close()
	}
	os.Setenv("PATH", strings.Join(parts, string(filepath.ListSeparator)))
}
