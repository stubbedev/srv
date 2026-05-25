//go:build !unix

package cmd

import "os"

func currentUID() int { return 0 }
func currentGID() int { return 0 }

func statUID(_ os.FileInfo) (uint32, bool) { return 0, false }
