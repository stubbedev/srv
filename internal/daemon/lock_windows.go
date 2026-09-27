//go:build windows

// Package daemon — lock_windows.go backs the single-instance lock with
// LockFileEx on the first byte of the lock file.
package daemon

import (
	"os"

	"golang.org/x/sys/windows"
)

func lockFileEx(f *os.File, flags uint32) error {
	var ol windows.Overlapped
	return windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, &ol)
}

func lockExclusive(f *os.File) error {
	return lockFileEx(f, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY)
}

func lockShared(f *os.File) error {
	return lockFileEx(f, windows.LOCKFILE_FAIL_IMMEDIATELY)
}

func unlockFile(f *os.File) error {
	var ol windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ol)
}
