// Package atomicfile replaces files so that readers see the old content or the new, never half.
package atomicfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// BackupSuffix names the copy of a file's original content, kept before spinup first changes it.
const BackupSuffix = ".spinup-backup"

// Write replaces path through a temp file in the same folder, writing through a symlink to its target.
// The temp name doesn't end in .json, so CLIProxyAPI's folder watcher never loads a half-written login.
func Write(path string, data []byte, perm os.FileMode) error {
	path = followLink(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), perm); err != nil {
		return err
	}
	return Rename(tmp.Name(), path)
}

// Rename waits out a reader that briefly holds the target open (or a file inside a folder being moved), which makes Windows refuse the rename.
func Rename(from, to string) error {
	err := os.Rename(from, to)
	for wait := 10 * time.Millisecond; err != nil && errors.Is(err, fs.ErrPermission) && wait <= 640*time.Millisecond; wait *= 2 {
		time.Sleep(wait)
		err = os.Rename(from, to)
	}
	return err
}

// WriteManaged is Write for a file the user also edits: it keeps the file's mode (newPerm when the file
// is new) and first copies the original to path+BackupSuffix, once.
func WriteManaged(path string, data []byte, newPerm os.FileMode) error {
	path = followLink(path)
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Write(path, data, newPerm)
	}
	if err != nil {
		return err
	}
	if err := backUpOnce(path, info.Mode().Perm()); err != nil {
		return err
	}
	return Write(path, data, info.Mode().Perm())
}

func backUpOnce(path string, perm os.FileMode) error {
	backup := path + BackupSuffix
	if _, err := os.Stat(backup); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return Write(backup, original, perm)
}

func followLink(path string) string {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if real, err := filepath.EvalSymlinks(path); err == nil {
			return real
		}
	}
	return path
}
