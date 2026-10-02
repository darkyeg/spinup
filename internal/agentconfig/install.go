package agentconfig

import (
	"github.com/darkyeg/spinup/internal/atomicfile"
	"github.com/darkyeg/spinup/internal/source"
)

const newFileMode = 0o644

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
	return atomicfile.WriteManaged(file.Path, []byte(file.Content), newFileMode)
}
