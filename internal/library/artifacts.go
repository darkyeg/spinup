package library

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/darkyeg/spinup/internal/atomicfile"
)

const ArtifactLimit = 32 << 20

func ValidDigest(digest string) bool {
	b, err := hex.DecodeString(digest)
	return err == nil && len(b) == sha256.Size && digest == strings.ToLower(digest)
}

func (l Library) artifactPath(digest string) (string, error) {
	if !ValidDigest(digest) {
		return "", errors.New("invalid artifact digest")
	}
	return l.Path("artifacts/" + digest + ".zip"), nil
}

func (l Library) PutArtifact(digest string, data []byte) error {
	p, err := l.artifactPath(digest)
	if err != nil {
		return err
	}
	if len(data) > ArtifactLimit {
		return fmt.Errorf("package exceeds %d MiB", ArtifactLimit>>20)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != digest {
		return errors.New("package content does not match its digest")
	}
	return atomicfile.Write(p, data, 0o600)
}

func (l Library) Artifact(digest string) ([]byte, error) {
	p, err := l.artifactPath(digest)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > ArtifactLimit {
		return nil, errors.New("invalid package artifact")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != digest {
		return nil, errors.New("package artifact is corrupt; run spinup packages fetch")
	}
	return data, nil
}

func (l Library) Artifacts(wanted []string) []string {
	var digests []string
	for _, digest := range wanted {
		if _, err := l.Artifact(digest); err == nil {
			digests = append(digests, digest)
		}
	}
	slices.Sort(digests)
	return slices.Compact(digests)
}
