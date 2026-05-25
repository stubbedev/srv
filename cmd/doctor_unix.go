//go:build unix

package cmd

import (
	"os"
	"syscall"
)

func currentUID() int { return os.Getuid() }
func currentGID() int { return os.Getgid() }

func statUID(info os.FileInfo) (uint32, bool) {
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return sys.Uid, true
}
