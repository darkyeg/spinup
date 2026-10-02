package main

import (
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/darkyeg/spinup/internal/config"
)

// stageBinary copies this program into the state folder, so the service doesn't depend on where
// it was downloaded. Windows can't replace a running binary, so there the copy waits as .new for
// the autostart step to swap in after stopping the service.
func stageBinary() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	dest := installedBinary()
	if sameFile(self, dest) {
		return dest, nil
	}
	if err := copyExecutable(self, dest+".new"); err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		return dest, nil
	}
	return dest, os.Rename(dest+".new", dest)
}

func installedBinary() string {
	name := "spinup"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(config.StateDir(), "bin", name)
}

func copyExecutable(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return err
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func sameFile(a, b string) bool {
	fa, errA := os.Stat(a)
	fb, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(fa, fb)
}
