// Package cmd implements the CLI commands for srv.
package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/ui"
)

var (
	// Version information - set at build time via ldflags
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"

	// Root command flags
	verbose bool
)

// RootCmd is the root command for srv.
var RootCmd = &cobra.Command{
	Use:   "srv",
	Short: "Manage containerized sites with Traefik",
	Long: `srv is a CLI tool for managing containerized sites with Traefik as a reverse proxy.
It supports both production domains (automatic Let's Encrypt SSL) and local development
(trusted *.test domains via mkcert).

Shell completion:
  source <(srv completion bash)   # Bash
  source <(srv completion zsh)    # Zsh
  srv completion fish | source    # Fish`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		ui.Verbose = verbose
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	RootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
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
func GetSiteNames() []string {
	sites, err := site.List()
	if err != nil {
		return nil
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
			return &s, nil
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
		return "local"
	}
	return "production"
}

// CommandExists checks if a command is available in PATH.
func CommandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// RunCommand executes a command without waiting for it to finish.
func RunCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	return cmd.Start()
}

// LoadParkedPaths loads the list of parked directories from config.
func LoadParkedPaths(cfg *config.Config) ([]string, error) {
	return cfg.GetParkedPaths()
}

// SaveParkedPaths saves the list of parked directories to config.
func SaveParkedPaths(cfg *config.Config, paths []string) error {
	return cfg.SetParkedPaths(paths)
}
