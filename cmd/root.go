// Package cmd implements the CLI commands for srv.
package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/ui"
)

// Command group IDs for organizing help output.
const (
	GroupSites  = "sites"
	GroupProxy  = "proxy"
	GroupSystem = "system"
)

var (
	// Version information - set at build time via ldflags
	Version   = constants.DefaultVersion
	Commit    = constants.DefaultCommit
	BuildDate = constants.DefaultBuildDate

	// Root command flags
	verbose bool
)

// RootCmd is the root command for srv.
var RootCmd = &cobra.Command{
	Use: constants.AppName,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		ui.Verbose = verbose
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	RootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")

	// Define command groups
	RootCmd.AddGroup(
		&cobra.Group{ID: GroupSites, Title: "Site Commands:"},
		&cobra.Group{ID: GroupProxy, Title: "Proxy Commands:"},
		&cobra.Group{ID: GroupSystem, Title: "System Commands:"},
	)
}

// Execute runs the root command.
func Execute() error {
	return RootCmd.Execute()
}

// SetVersion sets version information for the CLI.
func SetVersion(version, commit, buildDate string) {
	Version = version
	Commit = commit
	BuildDate = buildDate
}

// =============================================================================
// Shared Helpers
// =============================================================================

// GetSiteNames returns a list of all registered site names for shell completion.
// Returns an empty slice if sites cannot be listed (logs warning in verbose mode).
func GetSiteNames() []string {
	sites, err := site.List()
	if err != nil {
		ui.VerboseLog("Warning: could not list sites: %v", err)
		return []string{}
	}

	names := make([]string, 0, len(sites))
	for _, s := range sites {
		names = append(names, s.Name)
	}
	return names
}

// GetSiteFromArgs returns a site from args or detects from current directory.
// Returns nil, nil if no site is specified and cwd is not a registered site.
func GetSiteFromArgs(args []string) (*site.Site, error) {
	return getSiteFromArgsOrCwd(args, false)
}

// GetSiteFromArgsRequired returns a site from args or cwd, with error if not found.
// Use this when a site is required for the operation.
func GetSiteFromArgsRequired(args []string) (*site.Site, error) {
	return getSiteFromArgsOrCwd(args, true)
}

// getSiteFromArgsOrCwd is the internal implementation for site lookup.
func getSiteFromArgsOrCwd(args []string, required bool) (*site.Site, error) {
	if len(args) > 0 {
		return site.Get(args[0])
	}

	// Try to detect current directory site
	cwd, err := os.Getwd()
	if err != nil {
		if required {
			return nil, fmt.Errorf("no site specified and could not get current directory")
		}
		return nil, err
	}

	sites, err := site.List()
	if err != nil {
		return nil, err
	}

	for _, s := range sites {
		if s.Dir == cwd {
			// Return a copy to avoid loop variable pointer issue
			siteCopy := s
			return &siteCopy, nil
		}
	}

	if required {
		return nil, fmt.Errorf("no site specified and current directory is not a registered site")
	}
	return nil, nil
}

// TypeLabel returns "local" or "production" based on isLocal flag.
func TypeLabel(isLocal bool) string {
	if isLocal {
		return constants.TypeLabelLocal
	}
	return constants.TypeLabelProduction
}

// CommandExists checks if a command is available in PATH.
func CommandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// RunCommandDetached starts a command without waiting for it to finish.
// This is used for opening browsers and other GUI applications where we
// want to return control immediately without blocking on process completion.
// The started process will be orphaned (reparented to init) which is intentional.
func RunCommandDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	// Detach from parent process group so it doesn't get signals
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}
