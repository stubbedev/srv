// Package cmd — site_lifecycle.go implements the lifecycle commands
// (`srv start`, `srv stop`, `srv restart`) and the shared
// runBatchSiteOperation helper used by them and by `srv install`. The
// per-site work itself is site.Runner's; this file only words it.
package cmd

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/traefik"
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
	return lifecycleCmd{
		lifecycleVerb: verbStart,
		showURL:       true,
		// Apply any pending edge-config changes from a binary upgrade before
		// the first container starts, so a freshly-upgraded srv works without
		// `srv install`.
		onDockerReady: reconcileEdge,
		run:           func(r *site.Runner, s *site.Site) error { return r.Start(s, startFlags.build) },
	}.exec(args, startFlags.all)
}

// reconcileEdge re-renders the edge config when the binary changed since the
// last install. It may run from a batch worker, hence the Safe variants.
func reconcileEdge() {
	if reconciled, err := traefik.ReconcileVersion(Version); err != nil {
		ui.SafeWarn("Edge config reconcile failed: %v", err)
	} else if reconciled {
		ui.SafeIndentedDim(0, "Reconciled edge config to srv %s", Version)
	}
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
	return lifecycleCmd{
		lifecycleVerb: verbStop,
		run:           (*site.Runner).Stop,
	}.exec(args, stopFlags.all)
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
	return lifecycleCmd{
		lifecycleVerb: verbRestart,
		run:           func(r *site.Runner, s *site.Site) error { return r.Restart(s, restartFlags.build) },
	}.exec(args, restartFlags.all)
}

// =============================================================================
// Shared lifecycle flow
// =============================================================================

// lifecycleVerb is how the CLI words one lifecycle operation.
type lifecycleVerb struct {
	base   string // "start": error messages
	gerund string // "Starting": progress lines
	past   string // "started": success lines
}

var (
	verbStart   = lifecycleVerb{"start", "Starting", "started"}
	verbStop    = lifecycleVerb{"stop", "Stopping", "stopped"}
	verbRestart = lifecycleVerb{"restart", "Restarting", "restarted"}
)

// lifecycleCmd is one of `srv start|stop|restart`: the wording, and the
// site.Runner method that does the work for a single site.
type lifecycleCmd struct {
	lifecycleVerb
	showURL       bool   // print the site URL after a single-site success
	onDockerReady func() // see site.Runner.OnDockerReady
	run           func(r *site.Runner, s *site.Site) error
}

// exec runs the command for args[0], or for every registered site when all.
func (c lifecycleCmd) exec(args []string, all bool) error {
	r := &site.Runner{Quiet: all, OnDockerReady: c.onDockerReady}
	if all {
		sites, err := site.ListBasic()
		if err != nil {
			return err
		}
		if len(sites) == 0 {
			ui.Dim("No sites registered")
			return nil
		}
		ui.Info("%s %d site(s)...", c.gerund, len(sites))
		if err := runBatchSiteOperation(sites, c.lifecycleVerb, func(s *site.Site) error { return c.run(r, s) }); err != nil {
			return err
		}
		ui.Success("All sites %s", c.past)
		return nil
	}

	s, err := site.Require(args[0])
	if err != nil {
		return err
	}
	ui.Info("%s %s...", c.gerund, s.Name)
	if err := c.run(r, s); err != nil {
		return err
	}
	ui.Success("Site '%s' %s", s.Name, c.past)
	if d := s.Domain(); c.showURL && d != "" {
		ui.Info("https://%s", d)
	}
	return nil
}

// =============================================================================
// Batch operations helper
// =============================================================================

// runBatchSiteOperation runs an operation on multiple sites in parallel.
// Each failure is printed inline as it happens; the returned error names the
// failing sites so callers and tests can act on the set rather than just a count.
func runBatchSiteOperation(sites []site.Site, verb lifecycleVerb, op func(*site.Site) error) error {
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
		wg.Go(func() {
			for s := range siteChan {
				ui.SafeIndentedDim(1, "%s %s...", verb.gerund, s.Name)
				if err := op(&s); err != nil {
					ui.SafeError("Failed to %s %s: %v", verb.base, s.Name, err)
					failMu.Lock()
					failed = append(failed, s.Name)
					failMu.Unlock()
				}
			}
		})
	}

	// Send sites to workers
	for _, s := range validSites {
		siteChan <- s
	}
	close(siteChan)

	// Wait for all workers to complete
	wg.Wait()

	if len(failed) > 0 {
		slices.Sort(failed)
		return fmt.Errorf("failed to %s: %s", verb.base, strings.Join(failed, ", "))
	}
	return nil
}
