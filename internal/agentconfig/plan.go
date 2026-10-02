// Package agentconfig keeps the shared Claude Code and Codex configuration on this machine in step with the repo.
package agentconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/darkyeg/spinup/internal/host"
	"github.com/darkyeg/spinup/internal/source"
)

const (
	instructionsFile = "AGENTS.md"
	subagentsDir     = "agents/claude/agents"
	claudeSettings   = "agents/claude/settings.json"
	codexSettings    = "agents/codex/config.toml"
)

// Homes are the folders Claude Code and Codex read their configuration from.
type Homes struct{ Claude, Codex string }

// HostHomes are this machine's real folders.
func HostHomes() Homes { return Homes{Claude: host.ClaudeHome(), Codex: host.CodexHome()} }

// File is a managed file and the content it should have.
type File struct {
	Path    string
	Content string
}

// Plan lists every managed file with the content it should have on this machine.
func Plan(src source.Source, homes Homes) ([]File, error) {
	data := src.Data()
	instructions, err := instructionsText(data, src.Private())
	if err != nil {
		return nil, err
	}
	files := []File{
		{filepath.Join(homes.Claude, "CLAUDE.md"), instructions},
		{filepath.Join(homes.Codex, "AGENTS.md"), instructions},
	}
	subagents, err := subagentFiles(data, homes.Claude)
	if err != nil {
		return nil, err
	}
	files = append(files, subagents...)

	settings, err := settingsFile(data, homes.Claude)
	if err != nil {
		return nil, err
	}
	codex, err := codexFile(data, homes.Codex)
	if err != nil {
		return nil, err
	}
	return append(append(files, settings...), codex...), nil
}

func instructionsText(data, private fs.FS) (string, error) {
	text, err := fs.ReadFile(data, "agents/"+instructionsFile)
	if err != nil {
		return "", err
	}
	if private == nil {
		return string(text), nil
	}
	personal, err := fs.ReadFile(private, instructionsFile)
	if errors.Is(err, fs.ErrNotExist) {
		return string(text), nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(text), " \t\r\n") + "\n\n" + string(personal), nil
}

func subagentFiles(data fs.FS, claudeHome string) ([]File, error) {
	entries, err := fs.ReadDir(data, subagentsDir)
	if err != nil {
		return nil, err
	}
	var files []File
	for _, entry := range entries {
		if entry.IsDir() || path.Ext(entry.Name()) != ".md" {
			continue
		}
		text, err := fs.ReadFile(data, path.Join(subagentsDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		files = append(files, File{filepath.Join(claudeHome, "agents", entry.Name()), string(text)})
	}
	return files, nil
}

func settingsFile(data fs.FS, claudeHome string) ([]File, error) {
	target := filepath.Join(claudeHome, "settings.json")
	current, _, err := readManaged(target)
	if err != nil {
		return nil, err
	}
	if current == "" {
		current = "{}"
	}
	patch, err := fs.ReadFile(data, claudeSettings)
	if err != nil {
		return nil, err
	}
	merged, changed, err := mergeSettings([]byte(current), patch)
	if err != nil || !changed {
		return nil, err
	}
	return []File{{target, merged}}, nil
}

func codexFile(data fs.FS, codexHome string) ([]File, error) {
	target := filepath.Join(codexHome, "config.toml")
	current, _, err := readManaged(target)
	if err != nil {
		return nil, err
	}
	wanted, err := fs.ReadFile(data, codexSettings)
	if err != nil {
		return nil, err
	}
	edited, err := applyCodexSettings(current, string(wanted))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", target, err)
	}
	if edited == current {
		return nil, nil
	}
	return []File{{target, edited}}, nil
}

func readManaged(target string) (text string, exists bool, err error) {
	raw, err := os.ReadFile(target)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return strings.ReplaceAll(string(raw), "\r\n", "\n"), true, nil
}
