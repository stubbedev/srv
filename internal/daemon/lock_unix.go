//go:build unix

// Package daemon — lock_unix.go backs the single-instance lock with flock.
package daemon

import (
	"os"
	"syscall"
)

func flock(f *os.File, how int) error {
	ctl, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var flockErr error
	if cerr := ctl.Control(func(fd uintptr) {
		flockErr = syscall.Flock(int(fd), how|syscall.LOCK_NB)
	}); cerr != nil {
		return cerr
	}
	return flockErr
}

func lockExclusive(f *os.File) error {
	return flock(f, syscall.LOCK_EX)
}

func lockShared(f *os.File) error {
	return flock(f, syscall.LOCK_SH)
}

func unlockFile(f *os.File) error {
	return flock(f, syscall.LOCK_UN)
}
