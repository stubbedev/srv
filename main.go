// Package main provides the srv CLI application.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

var (
	// Version is set at build time
	Version = "dev"

	// Root command flags
	verbose bool
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
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
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")

	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(removeCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(restartCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(trustCmd)
	rootCmd.AddCommand(dnsCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(versionCmd)
}

// =============================================================================
// init command
// =============================================================================

var initFlags struct {
	fresh bool
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize srv (network, Traefik, start sites)",
	Long: `Initialize the srv environment:
  1. Creates the Docker network
  2. Generates Traefik configuration
  3. Starts Traefik container
  4. Starts all registered sites

Use --fresh to remove all existing configuration and start fresh.`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().BoolVar(&initFlags.fresh, "fresh", false, "Remove existing configuration and start fresh")
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

	// Determine total steps
	totalSteps := 3 // network, config, start traefik
	sites, _ := site.List()
	if len(sites) > 0 {
		totalSteps = 4 // + start sites
	}
	steps := ui.NewSteps(totalSteps)

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

	// Step 4: Start all sites (if any)
	if len(sites) > 0 {
		steps.Next("Starting %d site(s)", len(sites))
		startSites(sites)
		steps.Done("Sites started")
	}

	ui.Blank()
	ui.Success("srv initialized successfully!")
	ui.Info("Dashboard: %s", traefik.DashboardURL())

	// Check DNS status
	if !traefik.CheckSystemDNS() {
		ui.Blank()
		ui.Warn("Local DNS not configured")
		ui.Dim("Run 'srv dns setup' to enable *.test domain resolution")
	}

	return nil
}

func startSites(sites []site.Site) {
	for _, s := range sites {
		if s.IsBroken {
			ui.Warn("Skipping broken site: %s", s.Name)
			continue
		}

		ui.IndentedDim(1, "Starting %s...", s.Name)
		if err := docker.ComposeUp(s.Dir); err != nil {
			ui.Error("Failed to start %s: %v", s.Name, err)
		}
	}
}

// =============================================================================
// add command
// =============================================================================

var addFlags struct {
	domain         string
	port           string
	name           string
	service        string
	local          bool
	start          bool
	force          bool
	skipValidation bool
}

var addCmd = &cobra.Command{
	Use:   "add PATH",
	Short: "Add a site to srv",
	Long: `Register a new site with srv and generate Traefik configuration.

The PATH should be a directory containing a docker-compose.yml file.
If flags are not provided, you will be prompted interactively.`,
	Args: cobra.ExactArgs(1),
	RunE: runAdd,
}

func init() {
	addCmd.Flags().StringVarP(&addFlags.domain, "domain", "d", "", "Domain/hostname (e.g., example.com or myapp.test)")
	addCmd.Flags().StringVarP(&addFlags.port, "port", "p", "80", "Container port")
	addCmd.Flags().StringVarP(&addFlags.name, "name", "n", "", "Site name (default: directory name)")
	addCmd.Flags().StringVar(&addFlags.service, "service", "", "Service name in docker-compose")
	addCmd.Flags().BoolVarP(&addFlags.local, "local", "l", false, "Use local SSL (*.test domains)")
	addCmd.Flags().BoolVarP(&addFlags.start, "start", "s", false, "Start the site after adding")
	addCmd.Flags().BoolVarP(&addFlags.force, "force", "f", false, "Overwrite existing configuration")
	addCmd.Flags().BoolVar(&addFlags.skipValidation, "skip-validation", false, "Skip compose file validation")
}

func runAdd(cmd *cobra.Command, args []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Resolve path
	sitePath, err := site.ResolvePath(args[0])
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	// Check path exists and has compose file
	if _, err := os.Stat(sitePath); err != nil {
		return fmt.Errorf("path does not exist: %s", sitePath)
	}

	composePath, err := site.FindComposeFile(sitePath)
	if err != nil && !addFlags.skipValidation {
		return err
	}

	// Get service name
	serviceName := addFlags.service
	if serviceName == "" && composePath != "" {
		services, err := site.GetServiceNames(composePath)
		if err != nil {
			return fmt.Errorf("failed to parse compose file: %w", err)
		}

		if len(services) == 0 {
			return fmt.Errorf("no services found in compose file")
		}

		if len(services) == 1 {
			serviceName = services[0]
		} else {
			// Prompt for service
			var selected string
			form := huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[string]().
						Title("Select service").
						Description("Which service should Traefik route to?").
						Options(huh.NewOptions(services...)...).
						Value(&selected),
				),
			)
			if err := form.Run(); err != nil {
				return err
			}
			serviceName = selected
		}
	}

	// Get site name
	siteName := addFlags.name
	if siteName == "" {
		siteName = site.SanitizeName(sitePath)
	}

	// Check if site already exists
	if site.Exists(siteName) && !addFlags.force {
		return fmt.Errorf("site '%s' already exists. Use --force to overwrite", siteName)
	}

	// Get domain
	domain := addFlags.domain
	if domain == "" {
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Domain").
					Description("Enter the domain for this site").
					Placeholder("example.com or myapp.test").
					Value(&domain).
					Validate(func(s string) error {
						if s == "" {
							return fmt.Errorf("domain is required")
						}
						return nil
					}),
			),
		)
		if err := form.Run(); err != nil {
			return err
		}
	}

	// Determine if local
	isLocal := addFlags.local || site.IsLocalDomain(domain)

	// Write configuration files
	ui.Info("Configuring site: %s", siteName)

	if err := site.WriteEnvFile(sitePath, domain, isLocal, cfg.NetworkName); err != nil {
		return fmt.Errorf("failed to write env.site: %w", err)
	}

	if err := site.WriteSiteCompose(sitePath, serviceName, siteName, domain, addFlags.port, isLocal, cfg.NetworkName); err != nil {
		return fmt.Errorf("failed to write docker-compose.site.yml: %w", err)
	}

	// Generate SSL certificate for local domains
	if isLocal {
		if err := traefik.CheckMkcert(); err != nil {
			ui.Warn("Warning: %v", err)
			ui.Dim("Local HTTPS will not work without mkcert")
		} else if !traefik.IsCAInstalled() {
			ui.Warn("Warning: mkcert CA not installed")
			ui.Dim("Run 'srv trust' to install the CA")
		} else {
			if err := traefik.EnsureLocalCert(domain); err != nil {
				ui.Warn("Warning: Failed to generate certificate: %v", err)
			} else {
				ui.Dim("Generated SSL certificate for %s", domain)
				// Update Traefik dynamic config with new cert
				if err := traefik.UpdateDynamicConfig(); err != nil {
					ui.Warn("Warning: Failed to update Traefik config: %v", err)
				}
			}
		}
	}

	// Remove existing symlink if force
	if addFlags.force && site.Exists(siteName) {
		_ = site.Unregister(siteName)
	}

	// Register site
	if err := site.Register(siteName, sitePath); err != nil {
		return fmt.Errorf("failed to register site: %w", err)
	}

	ui.Success("Site '%s' added successfully!", siteName)
	ui.Dim("Domain: %s (%s)", domain, ui.Highlight(typeLabel(isLocal)))
	ui.Dim("Config: %s/docker-compose.site.yml", sitePath)

	// Add include to docker-compose.yml
	if composePath != "" {
		added, err := site.EnsureSiteComposeInclude(composePath)
		if err != nil {
			ui.Warn("Warning: Could not update %s: %v", filepath.Base(composePath), err)
			ui.Blank()
			ui.Warn("Add this to your docker-compose.yml manually:")
			ui.Code("  include:")
			ui.Code("    - docker-compose.site.yml")
		} else if added {
			ui.Dim("Added include to %s", filepath.Base(composePath))
		} else {
			ui.Dim("Include already present in %s", filepath.Base(composePath))
		}
	}

	// Start if requested
	if addFlags.start {
		ui.Blank()
		ui.Info("Starting site...")
		if err := docker.ComposeUp(sitePath); err != nil {
			return fmt.Errorf("failed to start site: %w", err)
		}
		ui.Success("Site is running at https://%s", domain)
	}

	return nil
}

func typeLabel(isLocal bool) string {
	if isLocal {
		return "local"
	}
	return "production"
}

// =============================================================================
// remove command
// =============================================================================

var removeCmd = &cobra.Command{
	Use:     "remove SITE",
	Aliases: []string{"rm"},
	Short:   "Remove a site from srv",
	Long:    `Stop a site's containers and remove it from srv.`,
	Args:    cobra.ExactArgs(1),
	RunE:    runRemove,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return getSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func runRemove(cmd *cobra.Command, args []string) error {
	siteName := args[0]

	s, err := site.Get(siteName)
	if err != nil {
		return err
	}

	// Stop containers if not broken
	if !s.IsBroken {
		ui.Info("Stopping containers...")
		if err := docker.ComposeDown(s.Dir); err != nil {
			ui.Warn("Warning: Failed to stop containers: %v", err)
		}

		// Remove include from docker-compose.yml
		composePath, err := site.FindComposeFile(s.Dir)
		if err == nil {
			if removed, err := site.RemoveSiteComposeInclude(composePath); err != nil {
				ui.Warn("Warning: Could not update %s: %v", filepath.Base(composePath), err)
			} else if removed {
				ui.Dim("Removed include from %s", filepath.Base(composePath))
			}
		}

		// Remove generated files
		site.RemoveGeneratedFiles(s.Dir)
	}

	// Remove SSL certificate for local domains
	if s.IsLocal && s.Domain != "" {
		if err := traefik.RemoveLocalCerts(s.Domain); err != nil {
			ui.Warn("Warning: Failed to remove certificate: %v", err)
		}
		// Update Traefik dynamic config
		if err := traefik.UpdateDynamicConfig(); err != nil {
			ui.Warn("Warning: Failed to update Traefik config: %v", err)
		}
	}

	// Remove symlink
	if err := site.Unregister(siteName); err != nil {
		return err
	}

	ui.Success("Site '%s' removed", siteName)
	return nil
}

// =============================================================================
// list command
// =============================================================================

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all registered sites",
	RunE:    runList,
}

func runList(cmd *cobra.Command, args []string) error {
	sites, err := site.List()
	if err != nil {
		return err
	}

	if len(sites) == 0 {
		ui.Dim("No sites registered. Use 'srv add PATH' to add a site.")
		return nil
	}

	// Sort by name
	sort.Slice(sites, func(i, j int) bool {
		return sites[i].Name < sites[j].Name
	})

	// Build table
	headers := []string{"NAME", "DOMAIN", "TYPE", "STATUS"}
	rows := make([][]string, 0, len(sites))

	for _, s := range sites {
		status := s.Status
		if s.IsBroken {
			status = "broken"
		}

		rows = append(rows, []string{
			s.Name,
			s.Domain,
			ui.TypeColor(s.IsLocal),
			ui.StatusColor(status),
		})
	}

	ui.PrintTable(headers, rows)
	return nil
}

// =============================================================================
// status command
// =============================================================================

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show srv status overview",
	Long: `Show comprehensive status information including:
  - Traefik status and dashboard URL
  - Number of registered sites
  - DNS configuration status
  - Local SSL certificate status`,
	RunE: runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	ui.Blank()

	// Traefik status
	if traefik.IsRunning() {
		ui.Success("Traefik is running")
		ui.IndentedDim(1, "Dashboard: %s", traefik.DashboardURL())
	} else {
		ui.Error("Traefik is not running")
		ui.IndentedDim(1, "Run 'srv init' to start")
	}

	ui.Blank()

	// Sites count
	sites, err := site.List()
	if err == nil {
		running := 0
		stopped := 0
		broken := 0
		for _, s := range sites {
			if s.IsBroken {
				broken++
			} else if s.Status == "running" {
				running++
			} else {
				stopped++
			}
		}

		if len(sites) == 0 {
			ui.Dim("No sites registered")
		} else {
			ui.Info("Sites: %d total", len(sites))
			if running > 0 {
				ui.IndentedDim(1, "%d running", running)
			}
			if stopped > 0 {
				ui.IndentedDim(1, "%d stopped", stopped)
			}
			if broken > 0 {
				ui.IndentedWarn(1, "%d broken", broken)
			}
		}
	}

	ui.Blank()

	// DNS status
	if traefik.IsDNSRunning() {
		if traefik.CheckSystemDNS() {
			ui.Success("DNS is configured")
			ui.IndentedDim(1, "*.test, *.local, *.localhost %s 127.0.0.1", ui.SymbolArrow)
		} else {
			ui.Warn("DNS server running but system not configured")
			ui.IndentedDim(1, "Run 'srv dns setup' to configure")
		}
	} else {
		ui.Warn("DNS server not running")
		ui.IndentedDim(1, "Run 'srv init' to start")
	}

	ui.Blank()

	// Certificate status
	certs := traefik.ListLocalCerts()
	if len(certs) == 0 {
		ui.Dim("No local SSL certificates")
		ui.IndentedDim(1, "Certificates are generated when adding .test/.local sites")
	} else {
		expired := 0
		expiringSoon := 0
		valid := 0
		for _, cert := range certs {
			if cert.IsExpired {
				expired++
			} else if cert.DaysLeft <= 30 {
				expiringSoon++
			} else {
				valid++
			}
		}

		if expired > 0 {
			ui.Error("Local SSL certificates: %d expired", expired)
		} else if expiringSoon > 0 {
			ui.Warn("Local SSL certificates: %d expiring soon", expiringSoon)
		} else {
			ui.Success("Local SSL certificates: %d valid", valid)
		}
		ui.IndentedDim(1, "Run 'srv trust' for details")
	}

	ui.Blank()

	return nil
}

// =============================================================================
// start command
// =============================================================================

var startCmd = &cobra.Command{
	Use:   "start SITE",
	Short: "Start a site",
	Args:  cobra.ExactArgs(1),
	RunE:  runStart,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return getSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func runStart(cmd *cobra.Command, args []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	s, err := site.Get(args[0])
	if err != nil {
		return err
	}

	if s.IsBroken {
		return fmt.Errorf("site '%s' is broken (target directory missing)", s.Name)
	}

	ui.Info("Starting %s...", s.Name)
	if err := docker.ComposeUp(s.Dir); err != nil {
		return fmt.Errorf("failed to start site: %w", err)
	}

	ui.Success("Site '%s' started", s.Name)
	if s.Domain != "" {
		ui.Info("https://%s", s.Domain)
	}
	return nil
}

// =============================================================================
// stop command
// =============================================================================

var stopCmd = &cobra.Command{
	Use:   "stop SITE",
	Short: "Stop a site",
	Args:  cobra.ExactArgs(1),
	RunE:  runStop,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return getSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func runStop(cmd *cobra.Command, args []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	s, err := site.Get(args[0])
	if err != nil {
		return err
	}

	if s.IsBroken {
		return fmt.Errorf("site '%s' is broken (target directory missing)", s.Name)
	}

	ui.Info("Stopping %s...", s.Name)
	if err := docker.ComposeStop(s.Dir); err != nil {
		return fmt.Errorf("failed to stop site: %w", err)
	}

	ui.Success("Site '%s' stopped", s.Name)
	return nil
}

// =============================================================================
// restart command
// =============================================================================

var restartCmd = &cobra.Command{
	Use:   "restart SITE",
	Short: "Restart a site",
	Args:  cobra.ExactArgs(1),
	RunE:  runRestart,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return getSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func runRestart(cmd *cobra.Command, args []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	s, err := site.Get(args[0])
	if err != nil {
		return err
	}

	if s.IsBroken {
		return fmt.Errorf("site '%s' is broken (target directory missing)", s.Name)
	}

	ui.Info("Restarting %s...", s.Name)
	if err := docker.ComposeRestart(s.Dir); err != nil {
		return fmt.Errorf("failed to restart site: %w", err)
	}

	ui.Success("Site '%s' restarted", s.Name)
	return nil
}

// =============================================================================
// logs command
// =============================================================================

var logsFlags struct {
	follow bool
	tail   string
	since  string
}

var logsCmd = &cobra.Command{
	Use:   "logs SITE",
	Short: "View logs for a site",
	Args:  cobra.ExactArgs(1),
	RunE:  runLogs,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return getSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFlags.follow, "follow", "f", false, "Follow log output")
	logsCmd.Flags().StringVar(&logsFlags.tail, "tail", "", "Number of lines to show from the end")
	logsCmd.Flags().StringVar(&logsFlags.since, "since", "", "Show logs since timestamp (e.g., 10m, 1h)")
}

func runLogs(cmd *cobra.Command, args []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	s, err := site.Get(args[0])
	if err != nil {
		return err
	}

	if s.IsBroken {
		return fmt.Errorf("site '%s' is broken (target directory missing)", s.Name)
	}

	// Build args
	composeArgs := []string{"logs"}
	if logsFlags.follow {
		composeArgs = append(composeArgs, "-f")
	}
	if logsFlags.tail != "" {
		composeArgs = append(composeArgs, "--tail", logsFlags.tail)
	}
	if logsFlags.since != "" {
		composeArgs = append(composeArgs, "--since", logsFlags.since)
	}

	return docker.Compose(s.Dir, composeArgs...)
}

// =============================================================================
// trust command
// =============================================================================

var trustFlags struct {
	force bool
}

var trustCmd = &cobra.Command{
	Use:   "trust",
	Short: "Install mkcert CA and list local SSL certificates",
	Long: `Install the mkcert CA certificate and show status of local SSL certificates.

Certificates are automatically generated for each .test/.local/.localhost domain
when you add a site with 'srv add'.

Requires mkcert to be installed:
  - macOS: brew install mkcert
  - Linux: https://github.com/FiloSottile/mkcert#linux

Use --force to regenerate all certificates.`,
	RunE: runTrust,
}

func init() {
	trustCmd.Flags().BoolVarP(&trustFlags.force, "force", "f", false, "Force regenerate all certificates")
}

func runTrust(cmd *cobra.Command, args []string) error {
	// Check mkcert is installed
	if err := traefik.CheckMkcert(); err != nil {
		return err
	}

	// Check CA status
	caInstalled := traefik.IsCAInstalled()
	if caInstalled {
		ui.Success("mkcert CA is installed")
	} else {
		ui.Info("Installing mkcert CA...")
		if err := traefik.InstallCA(); err != nil {
			return err
		}
		ui.Success("mkcert CA installed")
		ui.Blank()
		ui.Warn("Restart your browser for the CA to take effect")
	}

	ui.Blank()

	// List all local certificates
	certs := traefik.ListLocalCerts()
	if len(certs) == 0 {
		ui.Dim("No local SSL certificates")
		ui.Dim("Certificates are generated when adding .test/.local sites")
		return nil
	}

	ui.Info("Local SSL certificates:")
	regenerated := false
	for _, cert := range certs {
		if trustFlags.force {
			// Regenerate certificate
			ui.IndentedDim(1, "Regenerating %s...", cert.Domain)
			if err := traefik.GenerateLocalCert(cert.Domain); err != nil {
				ui.IndentedError(1, "Failed to regenerate %s: %v", cert.Domain, err)
			} else {
				ui.IndentedSuccess(1, "%s - regenerated", cert.Domain)
				regenerated = true
			}
		} else if cert.IsExpired {
			ui.IndentedError(1, "%s - EXPIRED", cert.Domain)
		} else if cert.DaysLeft <= 30 {
			ui.IndentedWarn(1, "%s - expires in %d days", cert.Domain, cert.DaysLeft)
		} else {
			ui.IndentedSuccess(1, "%s - valid (%d days)", cert.Domain, cert.DaysLeft)
		}
	}

	if regenerated {
		// Update dynamic config
		if err := traefik.UpdateDynamicConfig(); err != nil {
			ui.Warn("Warning: Failed to update Traefik config: %v", err)
		}

		// Restart Traefik if running
		if traefik.IsRunning() {
			ui.Blank()
			ui.Info("Restarting Traefik to load new certificates...")
			cfg, err := config.Load()
			if err == nil {
				_ = docker.ComposeRestart(cfg.TraefikDir)
			}
		}
	}

	return nil
}

// =============================================================================
// dns command
// =============================================================================

var dnsCmd = &cobra.Command{
	Use:   "dns [command]",
	Short: "Manage local DNS for *.test domains",
	Long: `Manage the local DNS server that resolves *.test, *.local, and *.localhost
domains to 127.0.0.1, eliminating the need to edit /etc/hosts.

Without a subcommand, shows current DNS status.`,
	RunE: runDNSStatus,
}

var dnsSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Configure system to use local DNS",
	Long: `Configure your system's DNS resolver to use the local DNS server
for *.test, *.local, and *.localhost domains.

This command requires sudo privileges to modify system DNS configuration.`,
	RunE: runDNSSetup,
}

var dnsRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove local DNS configuration",
	Long:  `Remove the DNS configuration that was set up by 'srv dns setup'.`,
	RunE:  runDNSRemove,
}

func init() {
	dnsCmd.AddCommand(dnsSetupCmd)
	dnsCmd.AddCommand(dnsRemoveCmd)
}

func runDNSStatus(cmd *cobra.Command, args []string) error {
	// Check if DNS container is running
	if traefik.IsDNSRunning() {
		ui.Success("DNS server is running (srv-dns)")
	} else {
		ui.Warn("DNS server is not running")
		ui.Dim("Run 'srv init' to start the DNS server")
		ui.Blank()
	}

	// Check if system DNS is configured
	if traefik.CheckSystemDNS() {
		ui.Success("System DNS is configured")
		ui.Dim("*.test, *.local, *.localhost %s 127.0.0.1", ui.SymbolArrow)
		return nil
	}

	// Check if local DNS server responds
	if traefik.CheckDNS() {
		ui.Warn("DNS server works but system is not configured to use it")
		ui.Dim("Resolver: %s", traefik.GetResolverName())
		ui.Blank()
		ui.Info("Run 'srv dns setup' to configure automatically")
	} else {
		ui.Warn("DNS is not configured")
		ui.Dim("Run 'srv init' first, then 'srv dns setup'")
	}

	return nil
}

func runDNSSetup(cmd *cobra.Command, args []string) error {
	// Check if DNS container is running
	if !traefik.IsDNSRunning() {
		return fmt.Errorf("DNS server is not running. Run 'srv init' first")
	}

	ui.Info("Configuring system DNS (%s)...", traefik.GetResolverName())
	ui.Dim("This may require your sudo password")
	ui.Blank()

	if err := traefik.SetupDNS(); err != nil {
		return err
	}

	// Verify it worked
	if traefik.CheckSystemDNS() {
		ui.Success("DNS configured successfully!")
		ui.Dim("*.test, *.local, *.localhost %s 127.0.0.1", ui.SymbolArrow)
	} else {
		ui.Warn("Configuration was applied but DNS resolution not yet working")
		ui.Dim("You may need to restart your browser or wait a few seconds")
	}

	return nil
}

func runDNSRemove(cmd *cobra.Command, args []string) error {
	ui.Info("Removing DNS configuration...")

	if err := traefik.RemoveDNS(); err != nil {
		return err
	}

	ui.Success("DNS configuration removed")
	return nil
}

// =============================================================================
// update command
// =============================================================================

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update Traefik to the latest version",
	Long: `Pull the latest Traefik image and restart the container.

This ensures you're running the latest Traefik version with security
patches and new features.`,
	RunE: runUpdate,
}

func runUpdate(cmd *cobra.Command, args []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	ui.Info("Pulling latest Traefik image...")
	if err := traefik.PullTraefikImage(); err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}

	if traefik.IsRunning() {
		ui.Info("Recreating Traefik container...")
		if err := traefik.RecreateTraefik(); err != nil {
			return fmt.Errorf("failed to restart Traefik: %w", err)
		}
		ui.Success("Traefik updated and restarted")
	} else {
		ui.Success("Traefik image updated")
		ui.Dim("Run 'srv init' to start Traefik")
	}

	return nil
}

// =============================================================================
// doctor command
// =============================================================================

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose common issues",
	Long: `Run diagnostic checks to identify common issues with your srv setup.

Checks performed:
  - Docker availability and status
  - Required ports (80, 443, 8080, 53)
  - Docker network existence
  - Traefik container status
  - DNS server status and configuration
  - Local SSL certificate validity
  - mkcert installation`,
	RunE: runDoctor,
}

func runDoctor(cmd *cobra.Command, args []string) error {
	ui.Blank()
	ui.Info("Running diagnostics...")
	ui.Blank()

	issues := 0

	// Check Docker
	ui.Bold("Docker")
	if err := docker.EnsureRunning(); err != nil {
		ui.IndentedError(1, "Docker is not running or not installed")
		issues++
	} else {
		ui.IndentedSuccess(1, "Docker is running")
	}

	ui.Blank()

	// Check ports
	ui.Bold("Ports")
	ports := []struct {
		port int
		name string
	}{
		{80, "HTTP"},
		{443, "HTTPS"},
		{8080, "Dashboard"},
		{53, "DNS"},
	}

	for _, p := range ports {
		if traefik.CheckPortAvailable(p.port) {
			ui.IndentedDim(1, ":%d (%s) - available", p.port, p.name)
		} else {
			// Check if it's our container using it
			if (p.port == 80 || p.port == 443 || p.port == 8080) && traefik.IsRunning() {
				ui.IndentedSuccess(1, ":%d (%s) - in use by Traefik", p.port, p.name)
			} else if p.port == 53 && traefik.IsDNSRunning() {
				ui.IndentedSuccess(1, ":%d (%s) - in use by srv-dns", p.port, p.name)
			} else {
				ui.IndentedWarn(1, ":%d (%s) - in use by another process", p.port, p.name)
				issues++
			}
		}
	}

	ui.Blank()

	// Check network
	ui.Bold("Docker Network")
	cfg, err := config.Load()
	if err != nil {
		ui.IndentedError(1, "Failed to load config: %v", err)
		issues++
	} else {
		if docker.NetworkExists(cfg.NetworkName) {
			ui.IndentedSuccess(1, "Network '%s' exists", cfg.NetworkName)
		} else {
			ui.IndentedWarn(1, "Network '%s' does not exist", cfg.NetworkName)
			ui.IndentedDim(1, "Run 'srv init' to create it")
			issues++
		}
	}

	ui.Blank()

	// Check Traefik
	ui.Bold("Traefik")
	if traefik.IsRunning() {
		ui.IndentedSuccess(1, "Container is running")
	} else {
		ui.IndentedWarn(1, "Container is not running")
		ui.IndentedDim(1, "Run 'srv init' to start")
		issues++
	}

	ui.Blank()

	// Check DNS
	ui.Bold("DNS Server")
	if traefik.IsDNSRunning() {
		ui.IndentedSuccess(1, "Container is running")

		if traefik.CheckDNS() {
			ui.IndentedSuccess(1, "Responding to queries")
		} else {
			ui.IndentedWarn(1, "Not responding to queries")
			issues++
		}

		if traefik.CheckSystemDNS() {
			ui.IndentedSuccess(1, "System DNS configured")
		} else {
			ui.IndentedWarn(1, "System DNS not configured")
			ui.IndentedDim(1, "Run 'srv dns setup' to configure")
			issues++
		}
	} else {
		ui.IndentedWarn(1, "Container is not running")
		ui.IndentedDim(1, "Run 'srv init' to start")
		issues++
	}

	ui.Blank()

	// Check SSL certificates
	ui.Bold("Local SSL Certificates")
	if err := traefik.CheckMkcert(); err != nil {
		ui.IndentedWarn(1, "mkcert is not installed")
		ui.IndentedDim(1, "Install mkcert for local HTTPS support")
		issues++
	} else {
		ui.IndentedSuccess(1, "mkcert is installed")

		if traefik.IsCAInstalled() {
			ui.IndentedSuccess(1, "CA is installed in system trust store")
		} else {
			ui.IndentedWarn(1, "CA not installed")
			ui.IndentedDim(1, "Run 'srv trust' to install")
			issues++
		}

		certs := traefik.ListLocalCerts()
		if len(certs) == 0 {
			ui.IndentedDim(1, "No local certificates (generated when adding .test/.local sites)")
		} else {
			expired := 0
			expiringSoon := 0
			for _, cert := range certs {
				if cert.IsExpired {
					expired++
				} else if cert.DaysLeft <= 30 {
					expiringSoon++
				}
			}

			if expired > 0 {
				ui.IndentedError(1, "%d certificate(s) EXPIRED", expired)
				ui.IndentedDim(1, "Run 'srv trust --force' to regenerate")
				issues++
			} else if expiringSoon > 0 {
				ui.IndentedWarn(1, "%d certificate(s) expiring soon", expiringSoon)
				issues++
			} else {
				ui.IndentedSuccess(1, "%d certificate(s) valid", len(certs))
			}
		}
	}

	ui.Blank()

	// Summary
	if issues == 0 {
		ui.Success("All checks passed!")
	} else {
		ui.Warn("%d issue(s) found", issues)
	}

	ui.Blank()

	return nil
}

// =============================================================================
// version command
// =============================================================================

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		ui.Info("srv %s", Version)
	},
}

// =============================================================================
// Helpers
// =============================================================================

func getSiteNames() []string {
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
