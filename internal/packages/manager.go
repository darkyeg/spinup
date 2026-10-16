package packages

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/darkyeg/spinup/internal/filelock"
	"github.com/darkyeg/spinup/internal/library"
)

type Manager struct {
	lib        library.Library
	targets    []Target
	resolve    resolver
	native     adapter
	targetRoot string
}

func New(lib library.Library, targets []Target) Manager {
	for i := range targets {
		if absolute, err := filepath.Abs(targets[i].Home); err == nil {
			targets[i].Home = absolute
		}
	}
	return Manager{lib: lib, targets: targets, resolve: defaultResolver(), native: native{}}
}

func (m Manager) lock(ctx context.Context) (*os.File, error) {
	return filelock.Acquire(ctx, m.lib.Path(".packages.lock"))
}

func (m Manager) Add(ctx context.Context, source, ref, subdir string, targets []string) (Package, error) {
	var added Package
	id, err := sourceID(source)
	if err != nil {
		return added, err
	}
	if ref == "" {
		ref = "HEAD"
	}
	lock, err := m.lock(ctx)
	if err != nil {
		return added, err
	}
	defer lock.Close()
	list, err := readManifest(m.lib)
	if err != nil {
		return added, err
	}
	if slices.ContainsFunc(list.Packages, func(p Package) bool { return p.Source == id }) {
		return added, fmt.Errorf("%s is already listed; use spinup packages update %s", id, id)
	}
	revision, err := m.resolve.Resolve(ctx, id, ref)
	if err != nil {
		return added, err
	}
	data, err := m.archive(ctx, id, revision)
	if err != nil {
		return added, err
	}
	p, err := unpack(data, subdir)
	if err != nil {
		return added, err
	}
	r, err := p.release(revision, digest(data), subdir, targets)
	if err != nil {
		return added, err
	}
	if err := m.lib.PutArtifact(r.Digest, data); err != nil {
		return added, err
	}
	added = Package{Source: id, Ref: ref, Release: r}
	list.Packages = append(list.Packages, added)
	return added, writeManifest(m.lib, list)
}

func (m Manager) Update(ctx context.Context, source, ref string) (Package, error) {
	var result Package
	id, err := sourceID(source)
	if err != nil {
		return result, err
	}
	lock, err := m.lock(ctx)
	if err != nil {
		return result, err
	}
	defer lock.Close()
	list, err := readManifest(m.lib)
	if err != nil {
		return result, err
	}
	index := slices.IndexFunc(list.Packages, func(p Package) bool { return p.Source == id })
	if index < 0 {
		return result, fmt.Errorf("%s is not listed", id)
	}
	result = list.Packages[index]
	if ref != "" {
		result.Ref = ref
	}
	revision, err := m.resolve.Resolve(ctx, id, result.Ref)
	if err != nil {
		return result, err
	}
	known := []Release{result.Release}
	if result.Previous != nil {
		known = append(known, *result.Previous)
	}
	data, err := m.archive(ctx, id, revision, known...)
	if err != nil {
		return result, err
	}
	p, err := unpack(data, result.Release.Path)
	if err != nil {
		return result, err
	}
	r, err := p.release(revision, digest(data), result.Release.Path, result.Release.Targets)
	if err != nil {
		return result, err
	}
	if r.Name != result.Release.Name {
		return result, errors.New("upstream changed the plugin identity; add it as a new package")
	}
	if err := m.lib.PutArtifact(r.Digest, data); err != nil {
		return result, err
	}
	if r.Digest != result.Release.Digest || r.Revision != result.Release.Revision {
		previous := result.Release
		result.Previous = &previous
		result.Release = r
	}
	if result.Ref == list.Packages[index].Ref && r.Digest == list.Packages[index].Release.Digest && r.Revision == list.Packages[index].Release.Revision {
		return result, nil
	}
	list.Packages[index] = result
	return result, writeManifest(m.lib, list)
}

func (m Manager) Rollback(ctx context.Context, source string) error {
	return m.edit(ctx, source, func(p *Package) error {
		if p.Previous == nil {
			return errors.New("no previous package revision is retained")
		}
		if _, err := m.payload(*p.Previous); err != nil {
			return err
		}
		old := p.Release
		p.Release = *p.Previous
		p.Previous = &old
		return nil
	}, false)
}

func (m Manager) Remove(ctx context.Context, source string) error {
	return m.edit(ctx, source, nil, true)
}

func (m Manager) edit(ctx context.Context, source string, change func(*Package) error, remove bool) error {
	id, err := sourceID(source)
	if err != nil {
		return err
	}
	lock, err := m.lock(ctx)
	if err != nil {
		return err
	}
	defer lock.Close()
	list, err := readManifest(m.lib)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(list.Packages, func(p Package) bool { return p.Source == id })
	if i < 0 {
		return fmt.Errorf("%s is not listed", id)
	}
	if remove {
		list.Packages = slices.Delete(list.Packages, i, i+1)
	} else if err := change(&list.Packages[i]); err != nil {
		return err
	}
	return writeManifest(m.lib, list)
}

func (m Manager) Fetch(ctx context.Context) error {
	lock, err := m.lock(ctx)
	if err != nil {
		return err
	}
	defer lock.Close()
	list, err := readManifest(m.lib)
	if err != nil {
		return err
	}
	for _, p := range list.Packages {
		for _, r := range []*Release{&p.Release, p.Previous} {
			if r == nil {
				continue
			}
			if _, err := m.lib.Artifact(r.Digest); err == nil {
				continue
			}
			data, err := m.resolve.Fetch(ctx, p.Source, r.Revision)
			if err != nil {
				return err
			}
			if err := m.lib.PutArtifact(r.Digest, data); err != nil {
				return fmt.Errorf("%s: locked download differs: %w", p.Source, err)
			}
		}
	}
	return nil
}

func (m Manager) List() ([]Package, error) {
	list, err := readManifest(m.lib)
	return list.Packages, err
}

func (m Manager) forTarget(t Target) Manager {
	m.targetRoot = "local/package-targets/" + targetKey(t) + "/packages/"
	return m
}

func targetKey(t Target) string {
	home := filepath.Clean(t.Home)
	if filepath.Separator == '\\' {
		home = strings.ToLower(home)
	}
	return digest([]byte(t.Name + "\x00" + home))
}
