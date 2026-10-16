package service

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/library"
	"github.com/darkyeg/spinup/internal/packages"
)

func (m *Machine) serveArtifact(w http.ResponseWriter, r *http.Request) {
	digest := strings.TrimPrefix(r.URL.Path, api.PathLibraryArtifact)
	if !library.ValidDigest(digest) {
		writeError(w, http.StatusBadRequest, "invalid artifact digest")
		return
	}
	if !m.library.artifacts.begin(artifactKey("serve", caller(r), digest), true) {
		writeError(w, http.StatusTooManyRequests, "artifact transfer budget exhausted or cooling down; run spinup packages fetch on the receiving machine to repair it")
		return
	}
	data, err := m.library.lib.Artifact(digest)
	if err != nil {
		writeError(w, http.StatusNotFound, "package artifact unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("ETag", `"`+digest+`"`)
	_, _ = w.Write(data)
}

func (m *Machine) acceptArtifact(w http.ResponseWriter, r *http.Request) {
	digest := strings.TrimPrefix(r.URL.Path, api.PathLibraryArtifact)
	if !library.ValidDigest(digest) {
		writeError(w, http.StatusBadRequest, "invalid artifact digest")
		return
	}
	required, err := packages.RequiredArtifacts(m.library.lib)
	if err != nil || !slices.Contains(required, digest) {
		writeError(w, http.StatusConflict, "artifact is not requested by this library")
		return
	}
	if _, err := m.library.lib.Artifact(digest); err == nil {
		writeJSON(w, okBody)
		return
	}
	if !m.library.artifacts.begin(artifactKey("receive", caller(r), digest), true) {
		writeError(w, http.StatusTooManyRequests, "artifact transfer budget exhausted or cooling down; run spinup packages fetch to repair it")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, library.ArtifactLimit))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "package artifact exceeds transfer limit")
		return
	}
	if err := m.library.lib.PutArtifact(digest, data); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	m.library.wantInstall()
	writeJSON(w, okBody)
}

func (m *Machine) exchangeArtifacts(ctx context.Context, base string, here, there api.LibraryState) {
	if there.Protocol < 2 {
		return
	}
	for _, d := range here.Required {
		key := artifactKey("get", base, d)
		if _, err := m.library.lib.Artifact(d); err == nil {
			m.library.artifacts.healthy(key)
			continue
		}
		if !slices.Contains(there.Artifacts, d) {
			continue
		}
		if !m.library.artifacts.begin(key, false) {
			continue
		}
		data, err := m.libraryClient().GetArtifact(ctx, base, d)
		if err == nil {
			err = m.library.lib.PutArtifact(d, data)
		}
		if err != nil {
			m.log.Printf("packages: artifact %s: %v (at most 3 automatic attempts; repair with spinup packages fetch)", d, err)
			continue
		}
		m.library.wantInstall()
		m.library.artifacts.succeeded(key)
	}
	for _, d := range there.Required {
		key := artifactKey("put", base, d)
		if slices.Contains(there.Artifacts, d) {
			m.library.artifacts.healthy(key)
			continue
		}
		if !slices.Contains(here.Artifacts, d) || !m.library.artifacts.begin(key, false) {
			continue
		}
		data, err := m.library.lib.Artifact(d)
		if err == nil {
			err = m.libraryClient().PutArtifact(ctx, base, d, data)
		}
		if err != nil {
			m.log.Printf("packages: sharing artifact %s: %v (at most 3 automatic attempts; repair with spinup packages fetch on the receiving machine)", d, err)
		} else {
			m.library.artifacts.succeeded(key)
		}
	}
}
