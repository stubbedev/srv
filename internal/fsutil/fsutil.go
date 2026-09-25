// Package fsutil holds small filesystem helpers shared across srv. The headline
// helper is AtomicWriteFile: every config file srv emits is watched by another
// process (Traefik, dnsmasq) or must survive a crash mid-write, so writes go
// through a temp file + rename rather than a truncating os.WriteFile.
package fsutil

import (
	"os"
	"path/filepath"

	"github.com/stubbedev/srv/internal/constants"
)

// AtomicWriteFile writes data to path atomically: it writes to a unique
// sibling temp file first, fsyncs it, then renames it over path. A reader
// watching path therefore never observes a truncated or half-written file, and
// a crash between the write and the rename leaves the original intact (the
// fsync means the renamed content is on disk, not just the rename).
//
// The temp file name is unique per call: concurrent writers to the same target
// (batch site operations, the daemon racing a CLI invocation) each get their
// own temp file instead of truncating each other's mid-write. The temp file is
// removed on a failed rename.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir, base := filepath.Split(path)
	tmp, err := os.CreateTemp(dir, base+constants.ExtTmp+"*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = tmp.Close(); _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	// Best effort: persist the rename itself so a crash right after cannot
	// resurrect the old contents on some filesystems.
	if d, derr := os.Open(dir); derr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
