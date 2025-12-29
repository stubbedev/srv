package cmd

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/daemon"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

var uninstallFlags struct {
	force bool
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Completely remove srv from the system",
	Long: `Completely remove srv and all its components from the system:
  1. Stops and removes the Traefik container
  2. Stops and removes the DNS container
  3. Removes system DNS configuration
  4. Removes the daemon service
  5. Removes the Docker network
  6. Removes the config directory (~/.config/srv)
  7. Removes the srv binary

WARNING: This will remove all srv configuration and registered sites.
Site directories and their contents are NOT removed.

Use --force to skip the confirmation prompt.`,
	RunE: runUninstall,
}

func init() {
	uninstallCmd.Flags().BoolVarP(&uninstallFlags.force, "force", "f", false, "Skip confirmation prompt")
	uninstallCmd.GroupID = GroupSystem
	RootCmd.AddCommand(uninstallCmd)
}

func runUninstall(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		// Config might not exist, that's okay
		cfg = nil
	}

	// Get executable path early so we can show it in the prompt
	executable, execErr := os.Executable()
	if execErr == nil {
		executable, _ = filepath.EvalSymlinks(executable)
	}

	// Confirmation prompt unless --force is used
	if !uninstallFlags.force {
		ui.Warn("This will completely remove srv from your system:")
		ui.Blank()
		ui.Print("  - Stop and remove Traefik and DNS containers")
		ui.Print("  - Remove system DNS configuration")
		ui.Print("  - Remove daemon service")
		ui.Print("  - Remove Docker network")
		if cfg != nil {
			ui.Print("  - Remove config directory: %s", cfg.Root)
		}
		if execErr == nil {
			ui.Print("  - Remove srv binary: %s", executable)
		}
		ui.Blank()
		ui.Dim("Site directories and their contents will NOT be removed.")
		ui.Blank()

		var confirmed bool
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Are you sure you want to uninstall srv?").
					Description("This action cannot be undone").
					Value(&confirmed),
			),
		)
		if err := form.Run(); err != nil {
			return err
		}

		if !confirmed {
			ui.Info("Uninstall cancelled")
			return nil
		}
	}

	ui.Blank()

	// Step 1: Stop Traefik and DNS containers
	ui.Info("Stopping containers...")
	if cfg != nil && (traefik.IsRunning() || traefik.IsDNSRunning()) {
		if err := docker.Compose(cfg.TraefikDir, "down"); err != nil {
			ui.Warn("Failed to stop containers: %v", err)
		} else {
			ui.Success("Containers stopped")
		}
	} else {
		ui.Dim("No containers running")
	}

	// Step 2: Remove system DNS configuration
	ui.Info("Removing DNS configuration...")
	if err := traefik.RemoveDNS(); err != nil {
		ui.Warn("Failed to remove DNS configuration: %v", err)
	} else {
		ui.Success("DNS configuration removed")
	}

	// Step 3: Remove daemon service
	ui.Info("Removing daemon service...")
	if daemon.IsInstalled() {
		if err := daemon.Uninstall(); err != nil {
			ui.Warn("Failed to remove daemon service: %v", err)
		} else {
			ui.Success("Daemon service removed")
		}
	} else if daemon.IsRunning() {
		// Daemon running but not as service, stop it
		if err := daemon.Stop(); err != nil {
			ui.Warn("Failed to stop daemon: %v", err)
		} else {
			ui.Success("Daemon stopped")
		}
	} else {
		ui.Dim("Daemon not installed")
	}

	// Step 4: Remove Docker network
	ui.Info("Removing Docker network...")
	if cfg != nil && docker.NetworkExists(cfg.NetworkName) {
		if err := removeDockerNetwork(cfg.NetworkName); err != nil {
			ui.Warn("Failed to remove network: %v", err)
		} else {
			ui.Success("Network removed: %s", cfg.NetworkName)
		}
	} else {
		ui.Dim("Network not found")
	}

	// Step 5: Remove config directory
	ui.Info("Removing config directory...")
	if cfg != nil {
		if err := os.RemoveAll(cfg.Root); err != nil {
			ui.Warn("Failed to remove config directory: %v", err)
		} else {
			ui.Success("Config directory removed: %s", cfg.Root)
		}
	} else {
		ui.Dim("Config directory not found")
	}

	// Step 6: Remove srv binary
	ui.Info("Removing srv binary...")
	if execErr != nil {
		ui.Warn("Could not determine binary path: %v", execErr)
	} else {
		if err := os.Remove(executable); err != nil {
			if os.IsPermission(err) {
				ui.Warn("Permission denied. Manually remove: %s", executable)
			} else {
				ui.Warn("Failed to remove binary: %v", err)
			}
		} else {
			ui.Success("Binary removed: %s", executable)
		}
	}

	ui.Blank()
	ui.Success("srv has been uninstalled")
	ui.Blank()

	return nil
}

// removeDockerNetwork removes a docker network.
func removeDockerNetwork(name string) error {
	cmd := exec.Command("docker", "network", "rm", name)
	return cmd.Run()
}
