// Package cmd — site_lifecycle.go implements the lifecycle commands
// (`srv start`, `srv stop`, `srv restart`) and the shared
// runBatchSiteOperation helper used by them and by `srv install`.
package cmd

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/ui"
)

// =============================================================================
// start command
// =============================================================================

var startFlags struct {
	all   bool
	build bool
}

var startCmd = &cobra.Command{
	Use:   "start SITE",
	Short: "Start a site",
	Long: `Start a site's containers.

Use --all to start all registered sites in parallel.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 && !startFlags.all {
			_ = cmd.Help()
			return ui.UsageError("srv start SITE", "a site name is required (or use --all to start every site)")
		}
		return nil
	},
	RunE: runStart,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	startCmd.Flags().BoolVarP(&startFlags.all, "all", "a", false, "Start all sites")
	startCmd.Flags().BoolVar(&startFlags.build, "build", false, "Rebuild images before starting")
	startCmd.GroupID = GroupSites
	RootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := docker.EnsureInitialized(cfg.NetworkName); err != nil {
		return err
	}

	if startFlags.all {
		return startAllSites()
	}

	s, err := site.GetByName(args[0])
	if err != nil {
		return err
	}

	if s.IsBroken {
		return fmt.Errorf("site '%s' is broken (target directory missing)", s.Name)
	}

	// Renew local SSL cert if needed
	if s.IsLocal && len(s.Domains) > 0 {
		renewLocalCertIfNeeded(s.Name, s.Domains, s.Wildcard)
	}

	ui.Info("Starting %s...", s.Name)
	// Use ComposeDir which is set correctly for both static and compose sites
	var startErr error
	if startFlags.build {
		startErr = docker.ComposeUpBuildWithProfile(s.ComposeDir, s.Profile)
	} else {
		startErr = docker.ComposeUpWithProfile(s.ComposeDir, s.Profile)
	}
	if startErr != nil {
		return fmt.Errorf("failed to start site: %w", startErr)
	}

	// For compose sites, connect service to traefik network after starting
	if s.Type == site.SiteTypeCompose && s.ComposeServiceName != "" {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if err := docker.ConnectServiceToNetwork(s.Dir, s.ComposeServiceName, cfg.NetworkName); err != nil {
			if errors.Is(err, docker.ErrServiceNotRunning) {
				ui.Dim("Service '%s' not running (may use Docker Compose profiles)", s.ComposeServiceName)
			} else {
				ui.Warn("Could not connect to traefik network: %v", err)
				ui.Dim("Run manually: docker network connect %s <container_name>", cfg.NetworkName)
			}
		}
	}

	ui.Success("Site '%s' started", s.Name)
	if d := s.Domain(); d != "" {
		ui.Info("https://%s", d)
	}
	return nil
}

// startAllSites starts all registered sites in parallel
func startAllSites() error {
	sites, err := site.List()
	if err != nil {
		return err
	}

	if len(sites) == 0 {
		ui.Dim("No sites registered")
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Renew any expiring local certs before starting
	for _, s := range sites {
		if s.IsLocal && len(s.Domains) > 0 && !s.IsBroken {
			renewLocalCertIfNeeded(s.Name, s.Domains, s.Wildcard)
		}
	}

	ui.Info("Starting %d site(s)...", len(sites))
	if err := runBatchSiteOperation(sites, "start", func(s *site.Site) error {
		// Use ComposeDir for docker operations with profile if set
		// Include --remove-orphans to clean up stale containers that may reference non-existent networks
		if err := docker.ComposeQuietWithProfile(s.ComposeDir, s.Profile, "up", "-d", "--remove-orphans"); err != nil {
			return err
		}
		// Connect compose sites to traefik network
		if s.Type == site.SiteTypeCompose && s.ComposeServiceName != "" {
			if err := docker.ConnectServiceToNetwork(s.Dir, s.ComposeServiceName, cfg.NetworkName); err != nil {
				// Only log actual errors, not "service not running" (profiles)
				if !errors.Is(err, docker.ErrServiceNotRunning) {
					ui.SafeError("Could not connect %s to traefik network: %v", s.Name, err)
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}
	ui.Success("All sites started")
	return nil
}

// =============================================================================
// stop command
// =============================================================================

var stopFlags struct {
	all bool
}

var stopCmd = &cobra.Command{
	Use:   "stop SITE",
	Short: "Stop a site",
	Long: `Stop a site's containers.

Use --all to stop all registered sites in parallel.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 && !stopFlags.all {
			_ = cmd.Help()
			return ui.UsageError("srv stop SITE", "a site name is required (or use --all to stop every site)")
		}
		return nil
	},
	RunE: runStop,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	stopCmd.Flags().BoolVarP(&stopFlags.all, "all", "a", false, "Stop all sites")
	stopCmd.GroupID = GroupSites
	RootCmd.AddCommand(stopCmd)
}

func runStop(cmd *cobra.Command, args []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	if stopFlags.all {
		return stopAllSites()
	}

	s, err := site.GetByName(args[0])
	if err != nil {
		return err
	}

	if s.IsBroken {
		return fmt.Errorf("site '%s' is broken (target directory missing)", s.Name)
	}

	ui.Info("Stopping %s...", s.Name)
	if err := docker.ComposeStop(s.ComposeDir); err != nil {
		return fmt.Errorf("failed to stop site: %w", err)
	}

	ui.Success("Site '%s' stopped", s.Name)
	return nil
}

// stopAllSites stops all registered sites in parallel
func stopAllSites() error {
	sites, err := site.List()
	if err != nil {
		return err
	}

	if len(sites) == 0 {
		ui.Dim("No sites registered")
		return nil
	}

	ui.Info("Stopping %d site(s)...", len(sites))
	if err := runBatchSiteOperation(sites, "stop", func(s *site.Site) error {
		return docker.ComposeStop(s.ComposeDir)
	}); err != nil {
		return err
	}
	ui.Success("All sites stopped")
	return nil
}

// =============================================================================
// restart command
// =============================================================================

var restartFlags struct {
	all   bool
	build bool
}

var restartCmd = &cobra.Command{
	Use:   "restart SITE",
	Short: "Restart a site",
	Long: `Restart a site's containers.

Use --all to restart all registered sites in parallel.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 && !restartFlags.all {
			_ = cmd.Help()
			return ui.UsageError("srv restart SITE", "a site name is required (or use --all to restart every site)")
		}
		return nil
	},
	RunE: runRestart,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	restartCmd.Flags().BoolVarP(&restartFlags.all, "all", "a", false, "Restart all sites")
	restartCmd.Flags().BoolVar(&restartFlags.build, "build", false, "Rebuild images before restarting")
	restartCmd.GroupID = GroupSites
	RootCmd.AddCommand(restartCmd)
}

func runRestart(cmd *cobra.Command, args []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := docker.EnsureInitialized(cfg.NetworkName); err != nil {
		return err
	}

	if restartFlags.all {
		return restartAllSites()
	}

	s, err := site.GetByName(args[0])
	if err != nil {
		return err
	}

	if s.IsBroken {
		return fmt.Errorf("site '%s' is broken (target directory missing)", s.Name)
	}

	ui.Info("Restarting %s...", s.Name)
	if restartFlags.build {
		if err := docker.ComposeUpBuildWithProfile(s.ComposeDir, s.Profile); err != nil {
			return fmt.Errorf("failed to rebuild and restart site: %w", err)
		}
	} else {
		if err := docker.ComposeRestart(s.ComposeDir); err != nil {
			return fmt.Errorf("failed to restart site: %w", err)
		}
	}

	ui.Success("Site '%s' restarted", s.Name)
	return nil
}

// restartAllSites restarts all registered sites in parallel
func restartAllSites() error {
	sites, err := site.List()
	if err != nil {
		return err
	}

	if len(sites) == 0 {
		ui.Dim("No sites registered")
		return nil
	}

	ui.Info("Restarting %d site(s)...", len(sites))
	if err := runBatchSiteOperation(sites, "restart", func(s *site.Site) error {
		return docker.ComposeRestart(s.ComposeDir)
	}); err != nil {
		return err
	}
	ui.Success("All sites restarted")
	return nil
}

// =============================================================================
// Batch operations helper
// =============================================================================

// runBatchSiteOperation runs an operation on multiple sites in parallel.
// Each failure is printed inline as it happens; the returned error names the
// failing sites so callers and tests can act on the set rather than just a count.
func runBatchSiteOperation(sites []site.Site, opName string, op func(*site.Site) error) error {
	// Filter out broken sites
	validSites := make([]site.Site, 0, len(sites))
	for _, s := range sites {
		if s.IsBroken {
			ui.Warn("Skipping broken site: %s", s.Name)
		} else {
			validSites = append(validSites, s)
		}
	}

	if len(validSites) == 0 {
		return nil
	}

	// Run operations in parallel with a worker pool
	workers := min(constants.MaxWorkers, len(validSites))

	var wg sync.WaitGroup
	var failMu sync.Mutex
	failed := make([]string, 0)
	siteChan := make(chan site.Site, len(validSites))

	// Start workers
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range siteChan {
				ui.SafeIndentedDim(1, "%s %s...", opName, s.Name)
				if err := op(&s); err != nil {
					ui.SafeError("Failed to %s %s: %v", opName, s.Name, err)
					failMu.Lock()
					failed = append(failed, s.Name)
					failMu.Unlock()
				}
			}
		}()
	}

	// Send sites to workers
	for _, s := range validSites {
		siteChan <- s
	}
	close(siteChan)

	// Wait for all workers to complete
	wg.Wait()

	if len(failed) > 0 {
		sort.Strings(failed)
		return fmt.Errorf("failed to %s: %s", opName, strings.Join(failed, ", "))
	}
	return nil
}
