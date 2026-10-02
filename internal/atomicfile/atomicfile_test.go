package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestWriteGoesThroughASymlinkToItsTarget(t *testing.T) {
	dir := t.TempDir()
	target, link := filepath.Join(dir, "real.txt"), filepath.Join(dir, "link.txt")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if err := Write(link, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced by a file: %v %v", info, err)
	}
	if got := read(t, target); got != "new" {
		t.Fatalf("target holds %q", got)
	}
}

func TestWriteManagedKeepsModeAndBacksUpOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"second", "third"} {
		if err := WriteManaged(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := read(t, path); got != "third" {
		t.Fatalf("file holds %q", got)
	}
	if got := read(t, path+BackupSuffix); got != "first" {
		t.Fatalf("backup holds %q, want the original", got)
	}
	if info, _ := os.Stat(path); runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode became %v", info.Mode().Perm())
	}
}

func TestWriteManagedCreatesANewFileWithoutABackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.json")
	if err := WriteManaged(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + BackupSuffix); err == nil {
		t.Fatal("a backup of nothing was made")
	}
}
