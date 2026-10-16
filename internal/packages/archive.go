package packages

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const expandedLimit = 64 << 20

type payloadFile struct {
	name string
	data []byte
	mode fs.FileMode
}
type payload struct{ files []payloadFile }

func safeRelative(name string) bool {
	if !fs.ValidPath(name) || name == "." || strings.ContainsAny(name, `\:*?"<>|`) {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || strings.EqualFold(part, ".git") {
			return false
		}
		stem := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || (len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '0' && stem[3] <= '9') {
			return false
		}
	}
	return true
}

func unpack(data []byte, subdir string) (payload, error) {
	var out payload
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return out, err
	}
	if len(z.File) == 0 || len(z.File) > 20000 {
		return out, errors.New("invalid package file count")
	}
	root, _, ok := strings.Cut(z.File[0].Name, "/")
	if !ok || !safeRelative(root) {
		return out, errors.New("package archive must have one repository root")
	}
	prefix := root + "/"
	if subdir != "" {
		if !safeRelative(subdir) {
			return out, errors.New("invalid package subdirectory")
		}
		prefix += subdir + "/"
	}
	seen := map[string]bool{}
	total := int64(0)
	for _, f := range z.File {
		if !strings.HasPrefix(f.Name, root+"/") {
			return out, errors.New("package has multiple archive roots")
		}
		name := strings.TrimSuffix(strings.TrimPrefix(f.Name, root+"/"), "/")
		if name == "" {
			continue
		}
		if !safeRelative(name) || (f.Mode().Type() != 0 && !f.Mode().IsDir()) {
			return out, fmt.Errorf("unsafe package entry %q", f.Name)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return out, fmt.Errorf("duplicate package entry %q", name)
		}
		seen[key] = true
		if f.UncompressedSize64 > expandedLimit || total+int64(f.UncompressedSize64) > expandedLimit {
			return out, errors.New("expanded package exceeds 64 MiB")
		}
		total += int64(f.UncompressedSize64)
		if f.FileInfo().IsDir() || !strings.HasPrefix(f.Name, prefix) {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return out, err
		}
		b, err := io.ReadAll(io.LimitReader(r, expandedLimit+1))
		r.Close()
		if err != nil {
			return out, err
		}
		if len(b) > expandedLimit {
			return out, errors.New("package file too large")
		}
		mode := fs.FileMode(0o644)
		if f.Mode()&0o111 != 0 {
			mode = 0o755
		}
		out.files = append(out.files, payloadFile{strings.TrimPrefix(f.Name, prefix), b, mode})
	}
	return out, nil
}

func (p payload) file(name string) ([]byte, bool) {
	for _, f := range p.files {
		if f.name == name {
			return f.data, true
		}
	}
	return nil, false
}

func (p payload) release(revision, digest, subdir string, targets []string) (Release, error) {
	r := Release{Revision: revision, Digest: digest, Path: subdir}
	paths := []struct{ target, path string }{{"claude-code", ".claude-plugin/plugin.json"}, {"codex", ".codex-plugin/plugin.json"}}
	for _, spec := range paths {
		data, ok := p.file(spec.path)
		if !ok && spec.target == "codex" {
			data, ok = p.file("plugin.json")
		}
		if !ok {
			continue
		}
		var meta struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		if err := json.Unmarshal(data, &meta); err != nil {
			return r, fmt.Errorf("%s: %w", spec.path, err)
		}
		if r.Name != "" && (r.Name != meta.Name || r.Version != meta.Version) {
			return r, errors.New("target manifests disagree on package name/version")
		}
		r.Name, r.Version = meta.Name, meta.Version
		if len(targets) == 0 || contains(targets, spec.target) {
			r.Targets = append(r.Targets, spec.target)
		}
	}
	for _, t := range targets {
		if !contains(r.Targets, t) {
			return r, fmt.Errorf("package has no %s plugin manifest", t)
		}
	}
	if err := validateRelease(r); err != nil {
		return r, fmt.Errorf("not a supported plugin package: %w", err)
	}
	return r, nil
}

func (p payload) write(dir string) error {
	for _, f := range p.files {
		name := filepath.Join(dir, filepath.FromSlash(f.name))
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(name, f.data, f.mode); err != nil {
			return err
		}
		if err := os.Chmod(name, f.mode); err != nil {
			return err
		}
	}
	return nil
}

func (p payload) verify(dir string) error {
	expected := map[string]payloadFile{}
	for _, f := range p.files {
		expected[f.name] = f
	}
	err := filepath.WalkDir(dir, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, name)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		f, ok := expected[rel]
		if !ok || !d.Type().IsRegular() {
			return fmt.Errorf("package content was changed: %s", rel)
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		if !bytes.Equal(data, f.data) {
			return fmt.Errorf("package content was changed: %s", rel)
		}
		delete(expected, rel)
		return nil
	})
	if err != nil {
		return err
	}
	if len(expected) != 0 {
		return errors.New("package content is missing files")
	}
	return nil
}

func contains(list []string, name string) bool {
	for _, item := range list {
		if item == name {
			return true
		}
	}
	return false
}

func (p payload) skillNames() []string {
	var names []string
	for _, f := range p.files {
		if strings.HasPrefix(f.name, "skills/") && path.Base(f.name) == "SKILL.md" {
			names = append(names, path.Base(path.Dir(f.name)))
		}
	}
	return names
}
