package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestAtomicWriteFile is the positive case: data lands at the target path and
// the temp sibling is gone afterwards.
func TestAtomicWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := AtomicWriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hi" {
		t.Errorf("contents = %q, want %q", data, "hi")
	}
	// Temp file must be cleaned up after a successful rename.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("tmp file remains: %v", err)
	}
}

// TestAtomicWriteFileOverwrite confirms an existing file is replaced wholesale.
func TestAtomicWriteFileOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("old-and-longer"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFile(path, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "new" {
		t.Errorf("contents = %q, want %q", data, "new")
	}
}

// TestAtomicWriteFileTempCreateFails is a negative case: the parent directory
// does not exist, so writing the .tmp file fails and the call errors.
func TestAtomicWriteFileTempCreateFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-subdir", "should-fail")
	if err := AtomicWriteFile(path, []byte("x"), 0o644); err == nil {
		t.Error("expected error when parent dir missing")
	}
}

// TestAtomicWriteFileRenameFails is a negative case: the destination is a
// non-empty directory, so the rename over it fails and the temp file is removed.
func TestAtomicWriteFileRenameFails(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "dest")
	if err := os.MkdirAll(filepath.Join(dest, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "sub", "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFile(dest, []byte("y"), 0o644); err == nil {
		t.Error("expected rename error over non-empty dir")
	}
	// On rename failure the temp file must not be left behind. With unique
	// temp names this can only be checked by pattern, not exact path.
	leftovers, _ := filepath.Glob(filepath.Join(dir, "dest.tmp*"))
	if len(leftovers) != 0 {
		t.Errorf("tmp files remain after failed rename: %v", leftovers)
	}
}

// TestAtomicWriteFileConcurrent proves concurrent writers to one target cannot
// tear each other's writes: each temp file is unique, so the final content is
// always one complete payload and no temp files survive.
func TestAtomicWriteFileConcurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := AtomicWriteFile(path, []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}

	const n = 16
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Go(func() {
			errs[i] = AtomicWriteFile(path, fmt.Appendf(nil, "payload-%02d", i), 0o644)
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d failed: %v", i, err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	matched := false
	for i := range n {
		if string(data) == fmt.Sprintf("payload-%02d", i) {
			matched = true
		}
	}
	if !matched {
		t.Fatalf("torn write: %q is not any single payload", data)
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, "f.tmp*"))
	if len(leftovers) != 0 {
		t.Errorf("tmp files remain after concurrent writes: %v", leftovers)
	}
}
