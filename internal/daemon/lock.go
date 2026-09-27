// Package daemon — lock.go is the daemon's single-instance guarantee: exactly
// one daemon process runs per srv root, and that process alone hosts the
// embedded DNS server and static file server for every site and container.
// The lock file also lets the standalone `srv dnsd` / `srv httpd` forms tell
// "the daemon serves this port" apart from any other bind conflict.
package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
)

// LockFile is the name of the daemon's single-instance lock file under the
// srv root.
const LockFile = "daemon.lock"

// daemonLock is a held single-instance lock. The OS drops it automatically
// when the process dies, so a crashed daemon never wedges the next start.
type daemonLock struct {
	file *os.File
}

func lockFilePath(cfg *config.Config) string {
	return filepath.Join(cfg.Root, LockFile)
}

// acquireDaemonLock takes an exclusive, non-blocking lock on
// <root>/daemon.lock. A second daemon fails here instead of running duplicate
// watchers and racing the first for the embedded servers' ports.
func acquireDaemonLock(cfg *config.Config) (*daemonLock, error) {
	path := lockFilePath(cfg)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, constants.FilePermDefault)
	if err != nil {
		return nil, fmt.Errorf("open daemon lock %s: %w", path, err)
	}
	if err := lockExclusive(f); err != nil {
		_ = f.Close()
		if pid := lockHolderPid(path); pid > 0 {
			return nil, fmt.Errorf("another srv daemon is already running (pid %d, lock %s): one daemon serves DNS and static sites for every site; stop it first with 'srv daemon stop'", pid, path)
		}
		return nil, fmt.Errorf("another srv daemon is already running (lock %s): one daemon serves DNS and static sites for every site; stop it first with 'srv daemon stop'", path)
	}
	if err := f.Truncate(0); err == nil {
		_, _ = f.WriteString(strconv.Itoa(os.Getpid()) + "\n")
	}
	return &daemonLock{file: f}, nil
}

// Release drops the lock. The file itself stays behind: unlinking it would
// let a new daemon lock a fresh inode while a waiter still holds the old one,
// breaking the one-instance guarantee the file exists for.
func (l *daemonLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := unlockFile(l.file)
	if cerr := l.file.Close(); err == nil {
		err = cerr
	}
	l.file = nil
	return err
}

// ServingPid reports the pid of the daemon process holding the
// single-instance lock, if any. Held means that process is the one serving
// the embedded DNS and static servers for the root.
func ServingPid(cfg *config.Config) (int, bool) {
	path := lockFilePath(cfg)
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer func() { _ = f.Close() }()
	if err := lockShared(f); err != nil {
		return lockHolderPid(path), true
	}
	_ = unlockFile(f)
	return 0, false
}

func lockHolderPid(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0
	}
	return pid
}
