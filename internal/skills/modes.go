package skills

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/darkyeg/spinup/internal/atomicfile"
)

const (
	keySkillOverrides  = "skillOverrides"
	userInvocableOnly  = "user-invocable-only"
	codexManualPolicy  = "policy:\n  allow_implicit_invocation: false\n"
	settingsFile       = "settings.json"
	codexPolicyDirName = "agents"
	codexPolicyFile    = "openai.yaml"
)

var userInvocableOnlyJSON = json.RawMessage(`"` + userInvocableOnly + `"`)

func overrideSettings(settings []byte, keep, manual []string) (updated []byte, changed bool, err error) {
	root, err := parseObject(settings)
	if err != nil {
		return nil, false, err
	}
	overrides := newObject()
	if raw, ok := root.get(keySkillOverrides); ok {
		if overrides, err = parseObject(raw); err != nil {
			return nil, false, err
		}
	}
	for _, name := range keep {
		current, has := overrides.get(name)
		ours := has && bytes.Equal(current, userInvocableOnlyJSON)
		switch {
		case slices.Contains(manual, name) && !ours:
			overrides.set(name, userInvocableOnlyJSON)
			changed = true
		case !slices.Contains(manual, name) && ours:
			overrides.remove(name)
			changed = true
		}
	}
	if !changed {
		return settings, false, nil
	}
	if len(overrides.keys) == 0 {
		root.remove(keySkillOverrides)
	} else {
		root.set(keySkillOverrides, overrides.compact())
	}
	return root.indented(), true, nil
}

func (p paths) applyModes(keep, manual []string) error {
	if err := p.writeClaudeOverrides(keep, manual); err != nil {
		return err
	}
	for _, name := range keep {
		if err := p.writeCodexPolicy(name, slices.Contains(manual, name)); err != nil {
			return err
		}
	}
	return nil
}

func (p paths) writeClaudeOverrides(keep, manual []string) error {
	path := filepath.Join(p.claude, settingsFile)
	current, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	updated, changed, err := overrideSettings(current, keep, manual)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return atomicfile.Write(path, updated, 0o644)
}

func (p paths) writeCodexPolicy(name string, manual bool) error {
	dir := filepath.Join(p.store, name)
	if _, err := os.Stat(dir); err != nil {
		return nil
	}
	policyDir := filepath.Join(dir, codexPolicyDirName)
	path := filepath.Join(policyDir, codexPolicyFile)
	current, err := os.ReadFile(path)
	exists := err == nil
	switch {
	case manual && !exists:
		return atomicfile.Write(path, []byte(codexManualPolicy), 0o644)
	case !manual && exists && string(current) == codexManualPolicy:
		if err := os.Remove(path); err != nil {
			return err
		}
		return removeIfEmpty(policyDir)
	}
	return nil
}

func removeIfEmpty(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) > 0 {
		return err
	}
	return os.Remove(dir)
}
