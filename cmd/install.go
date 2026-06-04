package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/daemon"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/firewall"
	"github.com/stubbedev/srv/internal/metrics"
	"github.com/stubbedev/srv/internal/mkcert"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
	"github.com/stubbedev/srv/internal/valet"
)

var installFlags struct {
	fresh bool
	yes   bool
	email string
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
	installCmd.Flags().BoolVarP(&installFlags.yes, "yes", "y", false, "Assume yes to every confirmable action (firewall open, port conflict auto-fix, valet stop, mkcert CA install retry). Required for non-interactive runs.")
	installCmd.Flags().StringVar(&installFlags.email, "email", "", "Let's Encrypt account email for production SSL. Stored on disk after first set; only required once. Pass an empty string to disable production SSL entirely.")
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

	// Pre-flight: a previously-installed Valet will own :80/:443/:53 and break
	// the port-bind step further down. Offer to stop its systemd units first
	// so the install can proceed without the user having to retry.
	if err := stopValetIfActive(); err != nil {
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

		if !installFlags.yes {
			steps.Skip("Firewall configuration skipped (pass --yes to open ports 80/443 via sudo)")
			ui.Warn("Note: Traefik may not be accessible without opening ports 80/443")
		} else if err := firewall.OpenPorts(); err != nil {
			ui.Warn("Failed to configure firewall: %v", err)
			ui.Dim("You may need to manually open ports 80 and 443")
		} else {
			steps.Done("Firewall configured")
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

	// Pull the Let's Encrypt email from --email (overrides any stored value)
	// or from a previous install. A local-only setup can ignore the error,
	// but production sites without an email fail later when ACME tries to
	// register, so we surface it up front unless --email "" was passed
	// explicitly.
	email, err := traefik.GetEmail(installFlags.email)
	if err != nil {
		ui.Warn("Let's Encrypt email not configured: %v", err)
		ui.Dim("Continuing with local sites only. Pass --email to enable production SSL.")
		email = ""
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

	// Re-up a previously-enabled metrics stack. Its routes/cert/DNS persist but
	// the containers do not survive a reboot, so without this grafana.local /
	// prometheus.local 502 until the user re-runs `srv metrics enable`.
	if metrics.IsConfigured(cfg) {
		steps.Next("Restarting metrics stack")
		if err := docker.ComposeUp(metrics.Dir(cfg)); err != nil {
			ui.Warn("Failed to restart metrics stack: %v", err)
			steps.Skip("Metrics stack skipped")
		} else {
			steps.Done("Metrics stack running (https://%s)", metrics.GrafanaDomain)
		}
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

// stopValetIfActive detects a running Valet install (config dir + systemd
// units owning ports 80/443/53). With --yes it stops the listed units;
// without --yes it logs a warning so the user knows why the next port-bind
// step will fail.
func stopValetIfActive() error {
	units, configDir := valet.Active()
	if len(units) == 0 {
		return nil
	}
	ui.Warn("Laravel Valet appears to be running")
	if configDir != "" {
		ui.Dim("  config: %s", configDir)
	}
	ui.Dim("  units:  %s", strings.Join(units, ", "))

	if !installFlags.yes {
		ui.Dim("Pass --yes to stop these units via sudo. Without it, srv will fail at the port-bind step.")
		return nil
	}
	if err := valet.Stop(units); err != nil {
		return fmt.Errorf("stop valet units: %w", err)
	}
	ui.Success("Stopped Valet units")
	return nil
}

// installCAWithRetry runs `mkcert -install`. With --yes it retries up to two
// times on sudo denial or a missing system-trust outcome (mkcert's own sudo
// re-prompt absorbs each retry). Without --yes a single attempt is made; any
// failure becomes a hard error. srv without a trusted local CA can't serve
// usable *.test URLs, so the install must fail loudly rather than warn and
// continue.
func installCAWithRetry() error {
	maxAttempts := 1
	if installFlags.yes {
		maxAttempts = 3
	}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		res, err := traefik.InstallCA()
		if err == nil && res.SystemTrustOK && !res.SudoDenied {
			reportCAInstall(res, true)
			return nil
		}
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
	}
	reportCAInstall(mkcert.InstallResult{}, true)
	if !installFlags.yes {
		return fmt.Errorf("mkcert CA was not installed; re-run `srv install --yes` to retry the sudo prompt up to three times, or install the CA manually")
	}
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
		var msg strings.Builder
		msg.WriteString("cannot start: the following ports are already in use\n")
		for _, c := range manual {
			if c.Process != "" {
				fmt.Fprintf(&msg, "\n  :%d (%s) is held by %s", c.Port, c.Name, c.Process)
			} else {
				fmt.Fprintf(&msg, "\n  :%d (%s) is held by an unknown process", c.Port, c.Name)
			}
			fmt.Fprintf(&msg, "\n    stop it with: %s\n", c.StopHint())
		}
		msg.WriteString("\nThen run 'srv install' again.")
		return fmt.Errorf("%s", msg.String())
	}

	// For fixable conflicts, describe them and bail unless --yes is passed.
	ui.Warn("The following ports are in use by processes srv can fix automatically:")
	for _, c := range fixable {
		ui.Dim("  :%d (%s) held by %s", c.Port, c.Name, c.Process)
		ui.Dim("    fix: %s", c.StopHint())
	}
	ui.Blank()

	if !installFlags.yes {
		return fmt.Errorf("port conflicts detected; re-run with --yes to auto-fix via sudo or stop the listed processes manually")
	}

	for _, c := range fixable {
		ui.Info("Fixing :%d (%s)...", c.Port, c.Name)
		if err := c.AutoFix(); err != nil {
			return fmt.Errorf("failed to fix :%d (%s): %w", c.Port, c.Name, err)
		}
	}

	// Re-check: give the OS a moment and verify the ports are now free.
	if remaining := traefik.CheckPortConflicts(); len(remaining) > 0 {
		var msg strings.Builder
		msg.WriteString("ports still in use after fix attempt:\n")
		for _, c := range remaining {
			fmt.Fprintf(&msg, "\n  :%d (%s) — stop it with: %s\n", c.Port, c.Name, c.StopHint())
		}
		msg.WriteString("\nRun 'srv install' again.")
		return fmt.Errorf("%s", msg.String())
	}

	return nil
}
