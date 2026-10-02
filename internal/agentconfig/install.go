package agentconfig

import (
	"errors"
	"io/fs"
	"os"

	"github.com/darkyeg/spinup/internal/atomicfile"
	"github.com/darkyeg/spinup/internal/source"
)

const (
	backupSuffix = ".spinup-backup"
	newFileMode  = 0o644
)

// Install writes the managed files that differ, backing each existing one up once; it returns the paths written.
func Install(src source.Source, homes Homes) ([]string, error) {
	files, err := Plan(src, homes)
	if err != nil {
		return nil, err
	}
	var written []string
	for _, file := range files {
		current, exists, err := readManaged(file.Path)
		if err != nil {
			return written, err
		}
		if exists && current == file.Content {
			continue
		}
		if err := write(file); err != nil {
			return written, err
		}
		written = append(written, file.Path)
	}
	return written, nil
}

// Drifted lists the managed files that are missing or differ from the repo.
func Drifted(src source.Source, homes Homes) ([]string, error) {
	files, err := Plan(src, homes)
	if err != nil {
		return nil, err
	}
	var drifted []string
	for _, file := range files {
		current, exists, err := readManaged(file.Path)
		if err != nil {
			return nil, err
		}
		if !exists || current != file.Content {
			drifted = append(drifted, file.Path)
		}
	}
	return drifted, nil
}

func write(file File) error {
	mode := os.FileMode(newFileMode)
	if info, err := os.Stat(file.Path); err == nil {
		mode = info.Mode().Perm()
		if err := backUpOnce(file.Path, mode); err != nil {
			return err
		}
	}
	return atomicfile.Write(file.Path, []byte(file.Content), mode)
}

func backUpOnce(target string, mode os.FileMode) error {
	backup := target + backupSuffix
	if _, err := os.Stat(backup); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	original, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	return atomicfile.Write(backup, original, mode)
}
