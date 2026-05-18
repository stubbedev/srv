package main

import (
	"os"
	"testing"

	"github.com/stubbedev/srv/cmd"
)

func TestRestoreCursor(t *testing.T) {
	restoreCursor()
}

func TestRun(t *testing.T) {
	prev := os.Args
	defer func() { os.Args = prev }()
	os.Args = []string{"srv", "version"}
	cmd.RootCmd.SetArgs([]string{"version"})
	defer cmd.RootCmd.SetArgs(nil)
	if got := run(); got != 0 {
		t.Errorf("run() = %d, want 0", got)
	}
}
