package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

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
	// Static site options
	spa   bool
	cache bool
	cors  bool
}

var addCmd = &cobra.Command{
	Use:   "add PATH",
	Short: "Add a site",
	Long: `Register a new site with srv and generate Traefik configuration.

If the PATH contains a docker-compose.yml file, srv will configure Traefik
to route traffic to the specified service.

If no docker-compose.yml is found, srv will serve the directory as static
files using nginx.

SSL certificates:
  - Use --local to generate a local certificate with mkcert
  - Without --local, Let's Encrypt will be used for production SSL

Examples:
  srv add /path/to/site --domain example.com          # Production with Let's Encrypt
  srv add /path/to/site --domain myapp.test --local   # Local dev with mkcert
  srv add . --domain example.com --start              # Add and start immediately`,
	Args: cobra.ExactArgs(1),
	RunE: runAdd,
}

func init() {
	addCmd.Flags().StringVarP(&addFlags.domain, "domain", "d", "", "Domain/hostname (e.g., example.com or myapp.test)")
	addCmd.Flags().StringVarP(&addFlags.port, "port", "p", "80", "Container port")
	addCmd.Flags().StringVarP(&addFlags.name, "name", "n", "", "Site name (default: directory name)")
	addCmd.Flags().StringVar(&addFlags.service, "service", "", "Service name in docker-compose")
	addCmd.Flags().BoolVarP(&addFlags.local, "local", "l", false, "Use local SSL via mkcert (otherwise Let's Encrypt)")
	addCmd.Flags().BoolVarP(&addFlags.start, "start", "s", false, "Start the site after adding")
	addCmd.Flags().BoolVarP(&addFlags.force, "force", "f", false, "Overwrite existing configuration")
	addCmd.Flags().BoolVar(&addFlags.skipValidation, "skip-validation", false, "Skip compose file validation")
	// Static site options
	addCmd.Flags().BoolVar(&addFlags.spa, "spa", true, "Enable SPA mode (fallback to index.html)")
	addCmd.Flags().BoolVar(&addFlags.cache, "cache", true, "Enable caching headers for static assets")
	addCmd.Flags().BoolVar(&addFlags.cors, "cors", false, "Enable CORS headers (allow all origins)")
	RootCmd.AddCommand(addCmd)
}

func runAdd(cmd *cobra.Command, args []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Validate and resolve site setup
	setup, err := validateSiteSetup(args[0])
	if err != nil {
		return err
	}

	// Prompt for any missing configuration
	if err := promptForMissingConfig(setup); err != nil {
		return err
	}

	// Validate inputs
	if err := validateSiteInputs(setup); err != nil {
		return err
	}

	// Write site configuration files
	if err := setupSiteFiles(cfg, setup); err != nil {
		return err
	}

	// Finalize setup (SSL, registration, start)
	return finalizeSiteSetup(cfg, setup)
}

// siteSetup holds all configuration needed for adding a site
type siteSetup struct {
	sitePath    string
	composePath string
	serviceName string
	siteName    string
	domain      string
	port        string
	isLocal     bool
	isStatic    bool // true if serving static files (no docker-compose.yml)
	// Static site options
	spa   bool
	cache bool
	cors  bool
}

// validateSiteSetup validates the path and discovers compose file
func validateSiteSetup(pathArg string) (*siteSetup, error) {
	sitePath, err := site.ResolvePath(pathArg)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	if _, err := os.Stat(sitePath); err != nil {
		return nil, fmt.Errorf("path does not exist: %s", sitePath)
	}

	setup := &siteSetup{
		sitePath: sitePath,
		port:     addFlags.port,
	}

	// Try to find a compose file - if not found, treat as static site
	composePath, err := site.FindComposeFile(sitePath)
	if err != nil {
		// No docker-compose file found - this will be a static site
		if !addFlags.skipValidation {
			setup.isStatic = true
		}
	} else {
		setup.composePath = composePath
	}

	return setup, nil
}

// promptForMissingConfig prompts user for any missing configuration
func promptForMissingConfig(setup *siteSetup) error {
	// Get service name (only for non-static sites)
	if !setup.isStatic {
		if err := promptForService(setup); err != nil {
			return err
		}
	}

	// Get site name
	setup.siteName = addFlags.name
	if setup.siteName == "" {
		setup.siteName = site.SanitizeName(setup.sitePath)
	}

	// Check if site already exists
	if site.Exists(setup.siteName) && !addFlags.force {
		return fmt.Errorf("site '%s' already exists. Use --force to overwrite", setup.siteName)
	}

	// Get domain
	if err := promptForDomain(setup); err != nil {
		return err
	}

	// Determine if local - require --local flag explicitly, don't auto-detect from domain
	setup.isLocal = addFlags.local

	// Static site options
	setup.spa = addFlags.spa
	setup.cache = addFlags.cache
	setup.cors = addFlags.cors

	return nil
}

// promptForService prompts for service selection if needed
func promptForService(setup *siteSetup) error {
	setup.serviceName = addFlags.service
	if setup.serviceName != "" || setup.composePath == "" {
		return nil
	}

	services, err := site.GetServiceNames(setup.composePath)
	if err != nil {
		return fmt.Errorf("failed to parse compose file: %w", err)
	}

	if len(services) == 0 {
		return fmt.Errorf("no services found in compose file")
	}

	if len(services) == 1 {
		setup.serviceName = services[0]
		return nil
	}

	// Prompt for service selection
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
	setup.serviceName = selected
	return nil
}

// promptForDomain prompts for domain input if not provided
func promptForDomain(setup *siteSetup) error {
	setup.domain = addFlags.domain
	if setup.domain != "" {
		return nil
	}

	var domain string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Domain").
				Description("Enter the domain for this site").
				Placeholder("example.com or myapp.test").
				Value(&domain).
				Validate(ValidateDomain),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}
	setup.domain = domain
	return nil
}

// validateSiteInputs validates all site inputs
func validateSiteInputs(setup *siteSetup) error {
	// Validate site name if explicitly provided
	if addFlags.name != "" {
		if err := ValidateSiteName(setup.siteName); err != nil {
			return err
		}
	}

	// Validate domain if provided via flag
	if addFlags.domain != "" {
		if err := ValidateDomain(setup.domain); err != nil {
			return err
		}
	}

	// Validate port
	if err := ValidatePort(setup.port); err != nil {
		return err
	}

	return nil
}

// setupSiteFiles writes configuration files for the site
func setupSiteFiles(cfg *config.Config, setup *siteSetup) error {
	if setup.isStatic {
		ui.Info("Configuring static site: %s", setup.siteName)
	} else {
		ui.Info("Configuring site: %s", setup.siteName)
	}

	if err := site.WriteEnvFile(setup.sitePath, setup.domain, setup.isLocal, cfg.NetworkName); err != nil {
		return fmt.Errorf("failed to write env.site: %w", err)
	}

	if setup.isStatic {
		// Static site: generate docker-compose.yml with nginx
		opts := site.StaticSiteOptions{
			SPA:   setup.spa,
			Cache: setup.cache,
			CORS:  setup.cors,
		}
		if err := site.WriteStaticSiteCompose(setup.sitePath, setup.siteName, setup.domain, cfg.NetworkName, setup.isLocal, opts); err != nil {
			return fmt.Errorf("failed to write docker-compose.yml for static site: %w", err)
		}
	} else {
		// Docker-compose site: generate docker-compose.site.yml overlay
		if err := site.WriteSiteCompose(setup.sitePath, setup.serviceName, setup.siteName, setup.domain, setup.port, setup.isLocal, cfg.NetworkName); err != nil {
			return fmt.Errorf("failed to write docker-compose.site.yml: %w", err)
		}
	}

	return nil
}

// finalizeSiteSetup handles SSL certs, registration, and optional start
func finalizeSiteSetup(cfg *config.Config, setup *siteSetup) error {
	// Generate SSL certificate for local domains
	if setup.isLocal {
		generateLocalCert(setup.domain)
	}

	// Remove existing symlink if force
	if addFlags.force && site.Exists(setup.siteName) {
		if err := site.Unregister(setup.siteName); err != nil {
			return fmt.Errorf("failed to remove existing site: %w", err)
		}
	}

	// Register site
	if err := site.Register(setup.siteName, setup.sitePath); err != nil {
		return fmt.Errorf("failed to register site: %w", err)
	}

	siteType := "static"
	if !setup.isStatic {
		siteType = "docker"
	}
	ui.Success("Site '%s' added successfully!", setup.siteName)
	ui.Dim("Domain: %s (%s, %s)", setup.domain, siteType, ui.Highlight(TypeLabel(setup.isLocal)))

	if setup.isStatic {
		ui.Dim("Config: %s/docker-compose.yml", setup.sitePath)
	} else {
		ui.Dim("Config: %s/docker-compose.site.yml", setup.sitePath)
		// Add include to docker-compose.yml (only for non-static sites)
		updateComposeInclude(setup.composePath)
	}

	// Start if requested
	if addFlags.start {
		return startSiteAfterAdd(setup)
	}

	return nil
}

// generateLocalCert generates SSL certificate for local domains and registers DNS
func generateLocalCert(domain string) {
	if err := traefik.CheckMkcert(); err != nil {
		ui.Warn("Warning: %v", err)
		ui.Dim("Local HTTPS will not work without mkcert")
		return
	}

	// Auto-install CA if not already installed
	if !traefik.IsCAInstalled() {
		ui.Dim("Installing mkcert CA...")
		if err := traefik.InstallCA(); err != nil {
			ui.Warn("Warning: Failed to install mkcert CA: %v", err)
			ui.Dim("Local HTTPS may not work in browsers")
		} else {
			ui.Success("mkcert CA installed")
			ui.Dim("Restart your browser for the CA to take effect")
		}
	}

	renewed, err := traefik.EnsureLocalCert(domain)
	if err != nil {
		ui.Warn("Warning: Failed to generate certificate: %v", err)
		return
	}

	if renewed {
		ui.Dim("Generated SSL certificate for %s", domain)
		if err := traefik.UpdateDynamicConfig(); err != nil {
			ui.Warn("Warning: Failed to update Traefik config: %v", err)
		}
	}

	// Register domain for local DNS
	if err := traefik.RegisterLocalDomain(domain); err != nil {
		ui.Warn("Warning: Failed to register DNS for %s: %v", domain, err)
	}
}

// renewLocalCertIfNeeded checks if a local cert needs renewal and renews it
func renewLocalCertIfNeeded(domain string) {
	cert := traefik.GetLocalCertInfo(domain)
	if !cert.Exists || cert.IsExpired || cert.DaysLeft <= traefik.RenewThresholdDays {
		if cert.IsExpired {
			ui.Dim("Renewing expired SSL certificate for %s...", domain)
		} else if cert.Exists && cert.DaysLeft <= traefik.RenewThresholdDays {
			ui.Dim("Renewing SSL certificate for %s (expires in %d days)...", domain, cert.DaysLeft)
		}

		if err := traefik.GenerateLocalCert(domain); err != nil {
			ui.Warn("Warning: Failed to renew certificate: %v", err)
			return
		}

		if err := traefik.UpdateDynamicConfig(); err != nil {
			ui.Warn("Warning: Failed to update Traefik config: %v", err)
		}
	}
}

// updateComposeInclude adds include directive to docker-compose.yml
func updateComposeInclude(composePath string) {
	if composePath == "" {
		return
	}

	added, err := site.EnsureSiteComposeInclude(composePath)
	if err != nil {
		ui.Warn("Warning: Could not update %s: %v", filepath.Base(composePath), err)
		ui.Blank()
		ui.Warn("Add this to your docker-compose.yml manually:")
		ui.Code("  include:")
		ui.Code("    - docker-compose.site.yml")
		return
	}

	if added {
		ui.Dim("Added include to %s", filepath.Base(composePath))
	} else {
		ui.Dim("Include already present in %s", filepath.Base(composePath))
	}
}

// startSiteAfterAdd starts the site after adding
func startSiteAfterAdd(setup *siteSetup) error {
	ui.Blank()
	ui.Info("Starting site...")
	if err := docker.ComposeUp(setup.sitePath); err != nil {
		return fmt.Errorf("failed to start site: %w", err)
	}
	ui.Success("Site is running at https://%s", setup.domain)
	return nil
}

// =============================================================================
// remove command
// =============================================================================

var removeCmd = &cobra.Command{
	Use:     "remove SITE",
	Aliases: []string{"rm"},
	Short:   "Remove a site",
	Long:    `Stop a site's containers and remove it from srv.`,
	Args:    cobra.ExactArgs(1),
	RunE:    runRemove,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	RootCmd.AddCommand(removeCmd)
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

		// For non-static sites, remove include from docker-compose.yml
		if !site.IsStaticSite(s.Dir) {
			composePath, err := site.FindComposeFile(s.Dir)
			if err == nil {
				if removed, err := site.RemoveSiteComposeInclude(composePath); err != nil {
					ui.Warn("Warning: Could not update %s: %v", filepath.Base(composePath), err)
				} else if removed {
					ui.Dim("Removed include from %s", filepath.Base(composePath))
				}
			}
		}

		// Remove generated files
		site.RemoveGeneratedFiles(s.Dir)
	}

	// Remove SSL certificate and DNS for local domains
	if s.IsLocal && s.Domain != "" {
		if err := traefik.RemoveLocalCerts(s.Domain); err != nil {
			ui.Warn("Warning: Failed to remove certificate: %v", err)
		}
		// Update Traefik dynamic config
		if err := traefik.UpdateDynamicConfig(); err != nil {
			ui.Warn("Warning: Failed to update Traefik config: %v", err)
		}
		// Unregister domain from local DNS
		if err := traefik.UnregisterLocalDomain(s.Domain); err != nil {
			ui.Warn("Warning: Failed to unregister DNS for %s: %v", s.Domain, err)
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
	Short:   "List sites",
	RunE:    runList,
}

func init() {
	RootCmd.AddCommand(listCmd)
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
	headers := []string{"NAME", "DOMAIN", "TYPE", "SSL", "STATUS"}
	rows := make([][]string, 0, len(sites))

	for _, s := range sites {
		status := s.Status
		if s.IsBroken {
			status = "broken"
		}

		// Determine SSL status
		sslStatus := getSSLStatus(s)

		rows = append(rows, []string{
			s.Name,
			s.Domain,
			ui.TypeColor(s.IsLocal),
			sslStatus,
			ui.StatusColor(status),
		})
	}

	ui.PrintTable(headers, rows)
	return nil
}

// getSSLStatus returns a formatted SSL status string for a site
func getSSLStatus(s site.Site) string {
	if s.IsBroken {
		return ui.DimText("-")
	}

	if s.IsLocal {
		// Local site - check mkcert certificate
		cert := traefik.GetLocalCertInfo(s.Domain)
		if !cert.Exists {
			return ui.StatusColor("missing")
		}
		if cert.IsExpired {
			return ui.StatusColor("expired")
		}
		if cert.DaysLeft <= 30 {
			return ui.StatusColor("expiring")
		}
		return ui.StatusColor("valid")
	}

	// Production site - Let's Encrypt (auto-managed)
	return ui.DimText("auto")
}

// =============================================================================
// info command
// =============================================================================

var infoCmd = &cobra.Command{
	Use:   "info [SITE]",
	Short: "Show site details",
	Long: `Display detailed information about a site including:
  - Site name and path
  - Domain and type (local/production)
  - Container status
  - SSL certificate status (for local sites)

If no site is specified and you're in a site directory, shows that site.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInfo,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	RootCmd.AddCommand(infoCmd)
}

func runInfo(cmd *cobra.Command, args []string) error {
	s, err := GetSiteFromArgsRequired(args)
	if err != nil {
		return err
	}

	ui.Blank()
	ui.Bold("Site: %s", s.Name)
	ui.Blank()

	// Basic info
	ui.Print("  Path:    %s", s.Dir)
	if s.Domain != "" {
		ui.Print("  Domain:  %s", s.Domain)
	}
	ui.Print("  Type:    %s", ui.TypeColor(s.IsLocal))

	// Status
	if s.IsBroken {
		ui.Print("  Status:  %s", ui.StatusColor("broken"))
		ui.IndentedWarn(1, "Target directory is missing")
	} else {
		ui.Print("  Status:  %s", ui.StatusColor(s.Status))
	}

	ui.Blank()

	// SSL certificate info for local sites
	if s.IsLocal && s.Domain != "" {
		showCertInfo(s.Domain)
	}

	// Show URL if running
	if s.Status == "running" && s.Domain != "" {
		ui.Blank()
		ui.Info("URL: https://%s", s.Domain)
	}

	ui.Blank()
	return nil
}

// showCertInfo displays SSL certificate information for a domain
func showCertInfo(domain string) {
	certs := traefik.ListLocalCerts()
	for _, cert := range certs {
		if cert.Domain == domain {
			ui.Bold("SSL Certificate")
			ui.Print("  Domain:  %s", cert.Domain)

			if cert.IsExpired {
				ui.Print("  Status:  %s", ui.StatusColor("expired"))
			} else if cert.DaysLeft <= 30 {
				ui.Print("  Status:  %s (%d days left)", ui.StatusColor("expiring"), cert.DaysLeft)
			} else {
				ui.Print("  Status:  %s (%d days left)", ui.StatusColor("valid"), cert.DaysLeft)
			}

			ui.Print("  Expires: %s", cert.ExpiresAt.Format("2006-01-02"))
			return
		}
	}

	ui.Bold("SSL Certificate")
	ui.Dim("  No certificate found for %s", domain)
	ui.IndentedDim(1, "Certificate will be generated on 'srv start'")
}

// =============================================================================
// status command
// =============================================================================

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status",
	Long: `Show comprehensive status information including:
  - Traefik status and dashboard URL
  - Number of registered sites
  - DNS configuration status
  - Local SSL certificate status`,
	RunE: runStatus,
}

func init() {
	RootCmd.AddCommand(statusCmd)
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
	}

	ui.Blank()

	return nil
}

// =============================================================================
// start command
// =============================================================================

var startFlags struct {
	all bool
}

var startCmd = &cobra.Command{
	Use:   "start [SITE]",
	Short: "Start a site",
	Long: `Start a site's containers.

Use --all to start all registered sites in parallel.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runStart,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	startCmd.Flags().BoolVarP(&startFlags.all, "all", "a", false, "Start all sites")
	RootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	if startFlags.all {
		return startAllSites()
	}

	if len(args) == 0 {
		return fmt.Errorf("site name required (or use --all)")
	}

	s, err := site.Get(args[0])
	if err != nil {
		return err
	}

	if s.IsBroken {
		return fmt.Errorf("site '%s' is broken (target directory missing)", s.Name)
	}

	// Renew local SSL cert if needed
	if s.IsLocal && s.Domain != "" {
		renewLocalCertIfNeeded(s.Domain)
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

	// Renew any expiring local certs before starting
	for _, s := range sites {
		if s.IsLocal && s.Domain != "" && !s.IsBroken {
			renewLocalCertIfNeeded(s.Domain)
		}
	}

	ui.Info("Starting %d site(s)...", len(sites))
	runBatchSiteOperation(sites, "start", func(s *site.Site) error {
		return docker.ComposeUp(s.Dir)
	})
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
	Use:   "stop [SITE]",
	Short: "Stop a site",
	Long: `Stop a site's containers.

Use --all to stop all registered sites in parallel.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runStop,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	stopCmd.Flags().BoolVarP(&stopFlags.all, "all", "a", false, "Stop all sites")
	RootCmd.AddCommand(stopCmd)
}

func runStop(cmd *cobra.Command, args []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	if stopFlags.all {
		return stopAllSites()
	}

	if len(args) == 0 {
		return fmt.Errorf("site name required (or use --all)")
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
	runBatchSiteOperation(sites, "stop", func(s *site.Site) error {
		return docker.ComposeStop(s.Dir)
	})
	ui.Success("All sites stopped")
	return nil
}

// =============================================================================
// restart command
// =============================================================================

var restartFlags struct {
	all bool
}

var restartCmd = &cobra.Command{
	Use:   "restart [SITE]",
	Short: "Restart a site",
	Long: `Restart a site's containers.

Use --all to restart all registered sites in parallel.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRestart,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	restartCmd.Flags().BoolVarP(&restartFlags.all, "all", "a", false, "Restart all sites")
	RootCmd.AddCommand(restartCmd)
}

func runRestart(cmd *cobra.Command, args []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	if restartFlags.all {
		return restartAllSites()
	}

	if len(args) == 0 {
		return fmt.Errorf("site name required (or use --all)")
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
	runBatchSiteOperation(sites, "restart", func(s *site.Site) error {
		return docker.ComposeRestart(s.Dir)
	})
	ui.Success("All sites restarted")
	return nil
}

// =============================================================================
// Batch operations helper
// =============================================================================

// runBatchSiteOperation runs an operation on multiple sites in parallel
func runBatchSiteOperation(sites []site.Site, opName string, op func(*site.Site) error) {
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
		return
	}

	// Run operations in parallel with a worker pool
	const maxWorkers = 4
	workers := min(maxWorkers, len(validSites))

	var wg sync.WaitGroup
	siteChan := make(chan site.Site, len(validSites))

	// Start workers
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range siteChan {
				ui.SafeIndentedDim(1, "%s %s...", opName, s.Name)
				if err := op(&s); err != nil {
					ui.SafeError("Failed to %s %s: %v", opName, s.Name, err)
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
	Short: "View site logs",
	Args:  cobra.ExactArgs(1),
	RunE:  runLogs,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFlags.follow, "follow", "f", false, "Follow log output")
	logsCmd.Flags().StringVar(&logsFlags.tail, "tail", "", "Number of lines to show from the end")
	logsCmd.Flags().StringVar(&logsFlags.since, "since", "", "Show logs since timestamp (e.g., 10m, 1h)")
	RootCmd.AddCommand(logsCmd)
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
