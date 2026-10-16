package packages

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/darkyeg/spinup/internal/atomicfile"
)

func marketplace(source string) string      { return "spinup-" + digest([]byte(source))[:24] }
func (m Manager) root(source string) string { return m.lib.Path(m.targetRoot + marketplace(source)) }
func (m Manager) content(source string, r Release) string {
	return filepath.Join(m.root(source), "revisions", r.Digest)
}

func (m Manager) payload(r Release) (payload, error) {
	data, err := m.lib.Artifact(r.Digest)
	if err != nil {
		return payload{}, err
	}
	p, err := unpack(data, r.Path)
	if err != nil {
		return p, err
	}
	actual, err := p.release(r.Revision, r.Digest, r.Path, r.Targets)
	if err != nil {
		return p, err
	}
	if actual.Name != r.Name || actual.Version != r.Version {
		return p, errors.New("artifact does not match the locked plugin manifest")
	}
	return p, nil
}

func (m Manager) materialize(source string, r Release) error {
	p, err := m.payload(r)
	if err != nil {
		return err
	}
	dest := m.content(source, r)
	if _, err := os.Lstat(dest); err == nil {
		return p.verify(dest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".stage-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := p.write(stage); err != nil {
		return err
	}
	return atomicfile.Rename(stage, dest)
}

func (m Manager) catalog(source string, r Release) error {
	name := marketplace(source)
	root := m.root(source)
	relative := "./revisions/" + r.Digest
	claude := map[string]any{"name": name, "owner": map[string]string{"name": "Spinup"}, "plugins": []any{map[string]any{"name": r.Name, "source": relative}}}
	codex := map[string]any{"name": name, "interface": map[string]string{"displayName": "Spinup: " + source}, "plugins": []any{map[string]any{"name": r.Name, "source": map[string]string{"source": "local", "path": relative}, "policy": map[string]string{"installation": "AVAILABLE", "authentication": "ON_INSTALL"}, "category": "Productivity"}}}
	for path, value := range map[string]any{".claude-plugin/marketplace.json": claude, ".agents/plugins/marketplace.json": codex} {
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		if err := atomicfile.Write(filepath.Join(root, filepath.FromSlash(path)), append(data, '\n'), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func samePath(a, b string) bool {
	x, err := filepath.Abs(a)
	if err != nil {
		return false
	}
	y, err := filepath.Abs(b)
	if err != nil {
		return false
	}
	if x == y {
		return true
	}
	i, err := os.Stat(x)
	if err != nil {
		return false
	}
	j, err := os.Stat(y)
	return err == nil && os.SameFile(i, j)
}

func (m Manager) checkContent(source string, r Release) error {
	p, err := m.payload(r)
	if err != nil {
		return fmt.Errorf("%w; run spinup packages fetch", err)
	}
	dir := m.content(source, r)
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return p.verify(dir)
}
