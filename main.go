// Package main provides the srv CLI application.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/stubbedev/srv/cmd"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/ui"
)

var (
	// Version information - set at build time via ldflags
	Version   = constants.DefaultVersion
	Commit    = constants.DefaultCommit
	BuildDate = constants.DefaultBuildDate
)

// restoreCursor outputs the ANSI escape sequence to show the cursor.
// This ensures the cursor is visible after the program exits,
// even if interrupted during interactive prompts.
func restoreCursor() {
	fmt.Print("\033[?25h")
}

func main() {
	// Skip cursor handling during shell completion to avoid polluting output
	isCompletion := len(os.Args) > 1 && os.Args[1] == "__complete"

	if !isCompletion {
		// Ensure cursor is always restored on exit
		defer restoreCursor()

		// Also restore cursor on signals (Ctrl+C, termination, etc.)
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
		go func() {
			<-sigChan
			restoreCursor()
			os.Exit(constants.ExitCodeError)
		}()
	}

	// Pass version info to cmd package
	cmd.SetVersion(Version, Commit, BuildDate)

	if err := cmd.Execute(); err != nil {
		ui.Error("%v", err)
		os.Exit(constants.ExitCodeError)
	}
}
