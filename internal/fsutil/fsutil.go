// Package fsutil holds small filesystem helpers shared across srv. The headline
// helper is AtomicWriteFile: every config file srv emits is watched by another
// process (Traefik, dnsmasq) or must survive a crash mid-write, so writes go
// through a temp file + rename rather than a truncating os.WriteFile.
package fsutil

import (
	"os"

	"github.com/stubbedev/srv/internal/constants"
)

// AtomicWriteFile writes data to path atomically: it writes to a sibling
// "<path>.tmp" first, then renames it over path. A reader watching path
// therefore never observes a truncated or half-written file, and a crash
// between the write and the rename leaves the original intact. The temp file is
// removed on a failed rename.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + constants.ExtTmp
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
