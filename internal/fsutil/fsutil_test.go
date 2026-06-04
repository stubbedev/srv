package fsutil

import (
	"os"
	"path/filepath"
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
	// On rename failure the temp file must not be left behind.
	if _, err := os.Stat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("tmp file remains after failed rename: %v", err)
	}
}
