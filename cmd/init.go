package cmd

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/daemon"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/firewall"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

var initFlags struct {
	fresh bool
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize srv environment",
	Long: `Initialize the srv environment:
  1. Creates the Docker network
  2. Generates Traefik configuration
  3. Starts Traefik container
  4. Installs the daemon service
  5. Starts all registered sites

Use --fresh to remove all existing configuration and start fresh.`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().BoolVar(&initFlags.fresh, "fresh", false, "Remove existing configuration and start fresh")
	initCmd.GroupID = GroupSystem
	RootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	// Handle fresh flag - reset everything
	if initFlags.fresh {
		ui.Warn("Removing existing configuration...")
		if err := traefik.Reset(); err != nil {
			return fmt.Errorf("failed to reset configuration: %w", err)
		}
		ui.Success("Configuration removed")
		ui.Blank()
	}

	// Check Docker is running
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Check firewall status
	fwStatus := firewall.CheckPorts()
	needFirewall := firewall.IsActive() && (!fwStatus.HTTPOpen || !fwStatus.HTTPSOpen)

	// Determine total steps
	totalSteps := constants.InitBaseSteps // network, config, start traefik
	if needFirewall {
		totalSteps++
	}
	sites, err := site.List()
	if err != nil {
		ui.VerboseLog("Warning: could not list sites: %v", err)
		sites = nil // Ensure sites is empty on error
	}
	if len(sites) > 0 {
		totalSteps++
	}
	// Add step for daemon installation
	needDaemon := !daemon.IsInstalled()
	if needDaemon {
		totalSteps++
	}
	steps := ui.NewSteps(totalSteps)

	// Step: Configure firewall if needed
	if needFirewall {
		steps.Next("Configuring firewall (%s)", firewall.Name(fwStatus.Firewall))
		ui.Dim("Ports 80 and 443 need to be opened for HTTP/HTTPS traffic")

		var openPorts bool
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Open ports 80 and 443?").
					Description("This requires sudo privileges").
					Value(&openPorts),
			),
		)
		if err := form.Run(); err != nil {
			return err
		}

		if openPorts {
			if err := firewall.OpenPorts(); err != nil {
				ui.Warn("Failed to configure firewall: %v", err)
				ui.Dim("You may need to manually open ports 80 and 443")
			} else {
				steps.Done("Firewall configured")
			}
		} else {
			steps.Skip("Firewall configuration skipped")
			ui.Warn("Note: Traefik may not be accessible without opening ports 80/443")
		}
	}

	// Step 1: Create network if needed
	steps.Next("Setting up Docker network")
	if !docker.NetworkExists(cfg.NetworkName) {
		if err := docker.CreateNetwork(cfg.NetworkName); err != nil {
			return fmt.Errorf("failed to create network: %w", err)
		}
		steps.Done("Created network: %s", cfg.NetworkName)
	} else {
		steps.Skip("Network %s already exists", cfg.NetworkName)
	}

	// Get or prompt for email
	email, err := traefik.GetEmail(true)
	if err != nil {
		return err
	}

	// Step 2: Generate Traefik config
	steps.Next("Configuring Traefik")
	if err := traefik.EnsureConfig(email); err != nil {
		return err
	}
	steps.Done("Traefik configured")

	// Step 3: Start Traefik
	steps.Next("Starting Traefik")
	if err := docker.ComposeUp(cfg.TraefikDir); err != nil {
		return fmt.Errorf("failed to start Traefik: %w", err)
	}
	steps.Done("Traefik started")

	// Step 4: Install daemon service
	if needDaemon {
		steps.Next("Installing daemon service")
		if err := daemon.Install(); err != nil {
			ui.Warn("Failed to install daemon service: %v", err)
			ui.Dim("Run 'srv daemon install' to try again later")
			steps.Skip("Daemon installation skipped")
		} else {
			steps.Done("Daemon service installed")
		}
	}

	// Step 5: Start all sites (if any)
	if len(sites) > 0 {
		steps.Next("Starting %d site(s)", len(sites))
		startSites(sites)
		steps.Done("Sites started")
	}

	ui.Blank()
	ui.Success("srv initialized successfully!")
	ui.Info("Dashboard: %s", traefik.DashboardURL())

	return nil
}

func startSites(sites []site.Site) {
	runBatchSiteOperation(sites, "Starting", func(s *site.Site) error {
		return docker.ComposeUp(s.Dir)
	})
}
