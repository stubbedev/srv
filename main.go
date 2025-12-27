// Package main provides the srv CLI application.
package main

import (
	"os"

	"github.com/stubbedev/srv/cmd"
)

var (
	// Version information - set at build time via ldflags
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

func main() {
	// Pass version info to cmd package
	cmd.SetVersion(Version, Commit, BuildDate)

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
