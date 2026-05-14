// Package main provides the srv CLI application.
package main

//go:generate go run ./cmd/gen-schema

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mattn/go-isatty"
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
// even if interrupted during interactive prompts. Writes to stderr
// so it never pollutes stdout (e.g. `srv completion zsh` output).
func restoreCursor() {
	fmt.Fprint(os.Stderr, "\033[?25h")
}

func main() {
	os.Exit(run())
}

func run() int {
	// Skip cursor handling when stderr isn't a TTY (e.g. shell completion,
	// piped output, generated completion scripts) to avoid emitting escape
	// sequences that would pollute the consumer.
	skipCursor := !isatty.IsTerminal(os.Stderr.Fd())

	if !skipCursor {
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
		return constants.ExitCodeError
	}
	return 0
}
