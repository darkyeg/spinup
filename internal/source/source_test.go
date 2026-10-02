package source

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestFindsTheCheckoutAboveAFolder(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"tools.json", "agents/AGENTS.md", "skills/skills.json"} {
		path := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	deep := filepath.Join(root, "docs", "deeper")
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPINUP_REPO", "")
	got := Find(deep)
	if dir, err := got.Checkout(); err != nil || dir != root {
		t.Fatalf("checkout %q, %v; want %q", dir, err, root)
	}
	if got.Private() != nil {
		t.Error("no local/ folder, yet a private repo was found")
	}
	if err := os.Mkdir(filepath.Join(root, PrivateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if Find(deep).Private() == nil {
		t.Error("local/ exists, yet no private repo was found")
	}
}

func TestTheBuiltInCopyHasTheData(t *testing.T) {
	data := Builtin().Data()
	for _, f := range []string{"tools.json", "agents/AGENTS.md", "skills/skills.json", "skills/per-repo.json"} {
		if _, err := fs.Stat(data, f); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	if _, err := Builtin().Checkout(); err != ErrNoCheckout {
		t.Errorf("the built-in copy claims a checkout: %v", err)
	}
}
