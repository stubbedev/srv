package cmd

import (
	"fmt"
	"strings"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/daemon"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/firewall"
	"github.com/stubbedev/srv/internal/mkcert"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

var installFlags struct {
	fresh bool
}

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install srv environment",
	Long: `Install the srv environment:
  1. Creates the Docker network
  2. Generates Traefik configuration
  3. Starts Traefik container
  4. Installs the daemon service
  5. Starts all registered sites

Use --fresh to remove all existing configuration and start fresh.`,
	RunE: runInstall,
}

func init() {
	installCmd.Flags().BoolVar(&installFlags.fresh, "fresh", false, "Remove existing configuration and start fresh")
	installCmd.GroupID = GroupSystem
	RootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	// Handle fresh flag - reset everything
	if installFlags.fresh {
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
		steps.Next("Configuring firewall (%s)", fwStatus.Firewall)
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

	// Pre-flight: check for port conflicts before attempting to bind.
	if conflicts := traefik.CheckPortConflicts(); len(conflicts) > 0 {
		if err := resolvePortConflicts(conflicts); err != nil {
			return err
		}
	}

	// /etc/resolv.conf may still point at a loopback DNS server the user just
	// stopped (e.g. a former Valet dnsmasq). Without working resolution the
	// next `docker compose up` can't pull Traefik/dnsmasq images. Swap in
	// public DNS for the duration of the pull, then restore the original
	// once srv's own dnsmasq is up and 127.0.0.1:53 resolves again.
	restoreResolv, rerr := traefik.EnsureBootstrapResolution()
	if rerr != nil {
		ui.Warn("Could not pre-swap /etc/resolv.conf: %v", rerr)
	} else if restoreResolv != nil {
		ui.Dim("Pre-swapped /etc/resolv.conf to public DNS for the image pull")
		defer restoreResolv()
	}

	if err := docker.ComposeUp(cfg.TraefikDir); err != nil {
		return fmt.Errorf("failed to start Traefik: %w", err)
	}
	steps.Done("Traefik started")

	// Pre-warm dnsmasq with every domain that's already registered so site
	// hostnames resolve immediately after `srv install` instead of waiting
	// for the first `srv add` to trigger a config reload.
	if err := traefik.UpdateDnsmasqConfig(); err != nil {
		ui.Dim("DNS pre-warm skipped: %v", err)
	}

	// Step 4: Set up dashboard HTTPS proxy (traefik.local)
	steps.Next("Setting up dashboard proxy (%s)", traefik.DashboardLocalURL())
	if err := traefik.CheckMkcert(); err != nil {
		steps.Skip("Dashboard proxy skipped (mkcert not available)")
		ui.Dim("Install mkcert to enable %s", traefik.DashboardLocalURL())
	} else {
		if !traefik.IsCAInstalled() {
			if err := installCAWithRetry(); err != nil {
				return err
			}
		}
		if err := traefik.SetupDashboardProxy(); err != nil {
			ui.Warn("Failed to set up dashboard proxy: %v", err)
			steps.Skip("Dashboard proxy setup failed")
		} else {
			steps.Done("Dashboard available at %s", traefik.DashboardLocalURL())
		}
	}

	// Step 5: Install daemon service
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

	// Step 6: Start all sites (if any)
	if len(sites) > 0 {
		steps.Next("Starting %d site(s)", len(sites))
		startSites(sites)
		steps.Done("Sites started")
	}

	ui.Blank()
	ui.Success("srv installed successfully!")
	ui.Info("Dashboard: %s", traefik.DashboardURL())
	ui.Info("Dashboard (HTTPS): %s", traefik.DashboardLocalURL())

	return nil
}

func startSites(sites []site.Site) {
	_ = runBatchSiteOperation(sites, "Starting", func(s *site.Site) error {
		return docker.ComposeUp(s.ComposeDir)
	})
}

// installCAWithRetry runs `mkcert -install` and, on sudo denial or a failed
// system-trust install, offers the user up to two retries via huh.Confirm.
// Returns nil on success or a hard error after the user declines retry — srv
// without a trusted local CA can't serve usable *.test URLs, so the install
// must fail loudly instead of warning and continuing.
func installCAWithRetry() error {
	const maxAttempts = 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		res, err := traefik.InstallCA()
		if err == nil && res.SystemTrustOK && !res.SudoDenied {
			reportCAInstall(res, true)
			return nil
		}
		// Surface the failure mode to the user.
		switch {
		case res.SudoDenied:
			ui.Warn("mkcert CA install: sudo authentication failed")
		case err != nil:
			ui.Warn("mkcert CA install: %v", err)
		case res.SystemUnsupported:
			ui.Warn("mkcert CA install: system trust store not supported on this platform")
			reportCAInstall(res, true)
			return fmt.Errorf("mkcert cannot install the CA on this platform — install it manually and re-run `srv install`")
		default:
			ui.Warn("mkcert CA install: did not land in the system trust store")
			if res.RawOutput != "" {
				ui.Dim("%s", strings.TrimSpace(res.RawOutput))
			}
		}

		if attempt == maxAttempts {
			break
		}
		var retry bool
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Try mkcert CA install again?").
					Description("Local TLS for *.test requires sudo to install the root CA. Decline to fail this install and finish CA setup yourself.").
					Value(&retry),
			),
		)
		if err := form.Run(); err != nil {
			return fmt.Errorf("mkcert CA install aborted: %w", err)
		}
		if !retry {
			break
		}
	}
	reportCAInstall(mkcert.InstallResult{}, true)
	return fmt.Errorf("mkcert CA was not installed — browsers will reject every *.test URL; install it manually and re-run `srv install`")
}

// resolvePortConflicts handles port conflicts detected before starting Traefik.
// For conflicts where srv knows how to fix them automatically it prompts the
// user to confirm, then applies the fix. For unknown processes it prints the
// manual steps and returns an error.
func resolvePortConflicts(conflicts []traefik.PortConflict) error {
	// Separate fixable conflicts from ones that need manual intervention.
	var fixable []traefik.PortConflict
	var manual []traefik.PortConflict
	for _, c := range conflicts {
		if c.CanAutoFix() {
			fixable = append(fixable, c)
		} else {
			manual = append(manual, c)
		}
	}

	// For manual conflicts, print instructions and return an error.
	if len(manual) > 0 {
		msg := "cannot start: the following ports are already in use\n"
		for _, c := range manual {
			if c.Process != "" {
				msg += fmt.Sprintf("\n  :%d (%s) is held by %s", c.Port, c.Name, c.Process)
			} else {
				msg += fmt.Sprintf("\n  :%d (%s) is held by an unknown process", c.Port, c.Name)
			}
			msg += fmt.Sprintf("\n    stop it with: %s\n", c.StopHint())
		}
		msg += "\nThen run 'srv install' again."
		return fmt.Errorf("%s", msg)
	}

	// For fixable conflicts, describe them and offer to fix.
	ui.Warn("The following ports are in use by processes srv can fix automatically:")
	for _, c := range fixable {
		ui.Dim("  :%d (%s) held by %s", c.Port, c.Name, c.Process)
		ui.Dim("    fix: %s", c.StopHint())
	}
	ui.Blank()

	var doFix bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Fix these conflicts automatically?").
				Description("This requires sudo privileges").
				Value(&doFix),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}
	if !doFix {
		return fmt.Errorf("port conflicts not resolved; run 'srv install' again after freeing the ports above")
	}

	for _, c := range fixable {
		ui.Info("Fixing :%d (%s)...", c.Port, c.Name)
		if err := c.AutoFix(); err != nil {
			return fmt.Errorf("failed to fix :%d (%s): %w", c.Port, c.Name, err)
		}
	}

	// Re-check: give the OS a moment and verify the ports are now free.
	if remaining := traefik.CheckPortConflicts(); len(remaining) > 0 {
		msg := "ports still in use after fix attempt:\n"
		for _, c := range remaining {
			msg += fmt.Sprintf("\n  :%d (%s) — stop it with: %s\n", c.Port, c.Name, c.StopHint())
		}
		msg += "\nRun 'srv install' again."
		return fmt.Errorf("%s", msg)
	}

	return nil
}
