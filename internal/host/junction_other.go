//go:build !windows

package host

import "os"

// LinkDir points link at target, with a symlink everywhere but Windows.
func LinkDir(link, target string) error { return os.Symlink(target, link) }
