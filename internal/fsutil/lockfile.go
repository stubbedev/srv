// Package fsutil — lockfile.go is the inter-process advisory lock shared by
// srv's writers. The CLI, the daemon and MCP tools are separate processes that
// run read-modify-write cycles over the same files (site metadata.yml, the
// local-domains registry, the certificate directories); an in-process mutex
// alone serializes only one of them, and the loser's update was silently
// dropped by the winner's write.
package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	// lockRetryInterval is how often a waiter re-checks the lock file.
	lockRetryInterval = 25 * time.Millisecond
	// lockTimeout bounds how long acquiring waits before failing.
	lockTimeout = 10 * time.Second
	// lockStaleAge is how old a lock file may be before it is considered
	// abandoned (its holder crashed without cleaning up) and broken. Every
	// holder refreshes nothing — the age is the mtime, set at creation — so
	// the age must exceed any legitimate hold; srv's critical sections run
	// in milliseconds, and 30s leaves three orders of margin.
	lockStaleAge = 30 * time.Second
)

// pathMu serializes WithFileLock callers within one process, keyed by lock
// path: two goroutines in the daemon never queue through the filesystem.
var pathMu sync.Map // map[string]*sync.Mutex

// WithFileLock runs fn while holding an exclusive advisory lock on path.
// The lock is a lock file created with O_CREATE|O_EXCL carrying the holder's
// PID — portable (no platform-specific flock), and crash-safe through the
// stale break below. fn runs with the lock held; on any return the lock file
// is removed. A lock file older than lockStaleAge is treated as abandoned and
// broken by the next acquirer.
func WithFileLock(path string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("lock %s: %w", path, err)
	}
	muAny, _ := pathMu.LoadOrStore(path, &sync.Mutex{})
	mu, ok := muAny.(*sync.Mutex)
	if !ok {
		return fmt.Errorf("lock %s: internal mutex table corrupted", path)
	}
	mu.Lock()
	defer mu.Unlock()

	if err := acquireLockFile(path); err != nil {
		return err
	}
	defer func() { _ = os.Remove(path) }()
	return fn()
}

// acquireLockFile blocks until the lock file at path belongs to this process.
func acquireLockFile(path string) error {
	deadline := time.Now().Add(lockTimeout)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = f.WriteString(strconv.Itoa(os.Getpid()))
			return f.Close()
		}
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("lock %s: %w", path, err)
		}
		st, statErr := os.Stat(path)
		switch {
		case statErr == nil && time.Since(st.ModTime()) > lockStaleAge:
			// Holder crashed without releasing: break the stale lock. Losing
			// the remove race with another waiter just costs one more loop.
			_ = os.Remove(path)
		case statErr != nil && !errors.Is(statErr, os.ErrNotExist):
			return fmt.Errorf("lock %s: %w", path, statErr)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("lock %s: timed out after %v waiting for another srv process", path, lockTimeout)
		}
		time.Sleep(lockRetryInterval)
	}
}
