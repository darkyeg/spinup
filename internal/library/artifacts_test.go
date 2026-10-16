package library

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
)

func TestArtifactsValidateAndDoNotTravelInsideSnapshot(t *testing.T) {
	lib := At(t.TempDir())
	data := []byte("archive")
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	if err := lib.PutArtifact(digest, data); err != nil {
		t.Fatal(err)
	}
	if err := lib.PutArtifact("../outside", data); err == nil {
		t.Fatal("invalid path accepted")
	}
	if err := lib.PutArtifact(digest, []byte("different")); err == nil {
		t.Fatal("digest mismatch accepted")
	}
	got, err := lib.Artifact(digest)
	if err != nil || !bytes.Equal(data, got) {
		t.Fatalf("%q %v", got, err)
	}
	files, err := lib.Snapshot()
	if err != nil || len(files) != 0 {
		t.Fatalf("artifact entered snapshot: %v %v", files, err)
	}
	path, _ := lib.artifactPath(digest)
	if err := os.WriteFile(path, []byte("damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := lib.Artifact(digest); err == nil {
		t.Fatal("corruption not detected")
	}
}
