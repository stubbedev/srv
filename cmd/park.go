package cmd

import (
	"fmt"
	"os"
	"slices"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

// =============================================================================
// park command
// =============================================================================

var parkCmd = &cobra.Command{
	Use:   "park",
	Short: "Manage parked directories",
	Long: `Register directories containing multiple sites.

When a directory is "parked", each subdirectory can be served as a site
automatically using its directory name as the domain (e.g., myapp -> myapp.test).`,
}

var parkAddCmd = &cobra.Command{
	Use:   "add [PATH]",
	Short: "Park a directory",
	Long: `Register a directory for automatic site discovery.

If no path is provided, the current directory is parked.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runParkAdd,
}

var parkRemoveCmd = &cobra.Command{
	Use:     "remove [PATH]",
	Aliases: []string{"forget"},
	Short:   "Remove parked directory",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runParkRemove,
}

var parkListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List parked directories",
	RunE:    runParkList,
}

func init() {
	parkCmd.AddCommand(parkAddCmd)
	parkCmd.AddCommand(parkRemoveCmd)
	parkCmd.AddCommand(parkListCmd)
	RootCmd.AddCommand(parkCmd)
}

// resolveParkPath resolves the park path from args or current directory.
func resolveParkPath(args []string) (string, error) {
	if len(args) > 0 {
		return site.ResolvePath(args[0])
	}
	return os.Getwd()
}

func runParkAdd(cmd *cobra.Command, args []string) error {
	parkPath, err := resolveParkPath(args)
	if err != nil {
		return err
	}

	// Verify it's a directory
	info, err := os.Stat(parkPath)
	if err != nil {
		return fmt.Errorf("path does not exist: %s", parkPath)
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", parkPath)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Load or create parked paths file
	parkedPaths, err := LoadParkedPaths(cfg)
	if err != nil {
		return err
	}

	// Check if already parked
	if slices.Contains(parkedPaths, parkPath) {
		ui.Info("Directory is already parked: %s", parkPath)
		return nil
	}

	// Add to parked paths
	parkedPaths = append(parkedPaths, parkPath)
	if err := SaveParkedPaths(cfg, parkedPaths); err != nil {
		return err
	}

	// Update and start the static server for parked directories
	ui.Info("Configuring static server...")
	if err := traefik.UpdateStaticServer(parkedPaths); err != nil {
		ui.Warn("Warning: Failed to configure static server: %v", err)
	} else {
		// Start or restart the static server
		if traefik.IsStaticServerRunning() {
			ui.Dim("Restarting static server...")
			if err := traefik.RestartStaticServer(); err != nil {
				ui.Warn("Warning: Failed to restart static server: %v", err)
			}
		} else {
			ui.Dim("Starting static server...")
			if err := traefik.StartStaticServer(); err != nil {
				ui.Warn("Warning: Failed to start static server: %v", err)
			}
		}
	}

	ui.Success("Parked directory: %s", parkPath)
	ui.Dim("Subdirectories are now accessible as {name}.test")
	ui.Dim("For Docker-based sites, use 'srv add' or 'srv link'")
	return nil
}

func runParkRemove(cmd *cobra.Command, args []string) error {
	parkPath, err := resolveParkPath(args)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	parkedPaths, err := LoadParkedPaths(cfg)
	if err != nil {
		return err
	}

	// Find and remove the path
	found := false
	newPaths := make([]string, 0, len(parkedPaths))
	for _, p := range parkedPaths {
		if p == parkPath {
			found = true
		} else {
			newPaths = append(newPaths, p)
		}
	}

	if !found {
		return fmt.Errorf("directory is not parked: %s", parkPath)
	}

	if err := SaveParkedPaths(cfg, newPaths); err != nil {
		return err
	}

	// Update or stop the static server
	if len(newPaths) == 0 {
		// No more parked directories, stop the static server
		ui.Info("Stopping static server (no parked directories)...")
		if err := traefik.StopStaticServer(); err != nil {
			ui.Warn("Warning: Failed to stop static server: %v", err)
		}
	} else {
		// Update and restart with remaining paths
		ui.Info("Updating static server configuration...")
		if err := traefik.UpdateStaticServer(newPaths); err != nil {
			ui.Warn("Warning: Failed to update static server: %v", err)
		} else if traefik.IsStaticServerRunning() {
			if err := traefik.RestartStaticServer(); err != nil {
				ui.Warn("Warning: Failed to restart static server: %v", err)
			}
		}
	}

	ui.Success("Unparked directory: %s", parkPath)
	return nil
}

func runParkList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	parkedPaths, err := LoadParkedPaths(cfg)
	if err != nil {
		return err
	}

	if len(parkedPaths) == 0 {
		ui.Dim("No directories parked. Use 'srv park add [PATH]' to park a directory.")
		return nil
	}

	ui.Bold("Parked directories:")
	for _, p := range parkedPaths {
		ui.Print("  %s", p)
	}
	return nil
}
