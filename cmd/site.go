package cmd

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
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
	port           int
	name           string
	service        string
	local          bool
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
to route traffic to the specified service. No files are created in the
project directory - all config is stored in ~/.config/srv.

If no docker-compose.yml is found, srv will serve the directory as static
files using nginx.

SSL certificates:
  - Use --local to generate a local certificate with mkcert
  - Without --local, Let's Encrypt will be used for production SSL

Examples:
  srv add /path/to/site --domain example.com          # Production with Let's Encrypt
  srv add /path/to/site --domain myapp.test --local   # Local dev with mkcert
  srv add . --domain example.com --start              # Add and start immediately
  srv add /path/to/static --domain site.test --local  # Static files with nginx`,
	Args: cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if addFlags.domain == "" {
			cmd.Help()
			os.Exit(1)
		}
		return nil
	},
	RunE: runAdd,
}

func init() {
	addCmd.Flags().StringVarP(&addFlags.domain, "domain", "d", "", "Domain/hostname (e.g., example.com or myapp.test)")
	addCmd.Flags().IntVarP(&addFlags.port, "port", "p", constants.DefaultContainerPort, "Container port")
	addCmd.Flags().StringVarP(&addFlags.name, "name", "n", "", "Site name (default: directory name)")
	addCmd.Flags().StringVar(&addFlags.service, "service", "", "Container name to route to")
	addCmd.Flags().BoolVarP(&addFlags.local, "local", "l", false, "Use local SSL via mkcert (otherwise Let's Encrypt)")
	addCmd.Flags().BoolVarP(&addFlags.force, "force", "f", false, "Overwrite existing configuration")
	addCmd.Flags().BoolVar(&addFlags.skipValidation, "skip-validation", false, "Skip compose file validation")
	// Static site options
	addCmd.Flags().BoolVar(&addFlags.spa, "spa", true, "Enable SPA mode (fallback to index.html)")
	addCmd.Flags().BoolVar(&addFlags.cache, "cache", true, "Enable caching headers for static assets")
	addCmd.Flags().BoolVar(&addFlags.cors, "cors", false, "Enable CORS headers (allow all origins)")
	addCmd.GroupID = GroupSites
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
	sitePath           string
	composePath        string
	serviceName        string // Container name for Traefik routing
	composeServiceName string // Docker Compose service name for compose commands
	profile            string // Docker Compose profile (if service uses profiles)
	siteName           string
	domain             string
	port               int
	isLocal            bool
	isStatic           bool // true if serving static files (no docker-compose.yml)
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
		// No docker-compose file found - treat as static site
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

	// Get domain first (needed for site name)
	if err := promptForDomain(setup); err != nil {
		return err
	}

	// Get site name - use domain by default for uniqueness
	setup.siteName = addFlags.name
	if setup.siteName == "" {
		setup.siteName = site.SanitizeName(setup.domain)
	}

	// Check if site already exists
	if site.Exists(setup.siteName) && !addFlags.force {
		return fmt.Errorf("site '%s' already exists. Use --force to overwrite", setup.siteName)
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
	if setup.composePath == "" {
		return nil
	}

	services, err := site.GetServiceInfos(setup.composePath)
	if err != nil {
		return fmt.Errorf("failed to parse compose file: %w", err)
	}

	if len(services) == 0 {
		return fmt.Errorf("no services found in compose file")
	}

	// Helper to set service info including discovered port
	setServiceInfo := func(svc site.ServiceInfo) {
		setup.serviceName = svc.ContainerName
		setup.composeServiceName = svc.ServiceName
		// Use discovered port if user didn't explicitly set one
		if svc.Port > 0 && setup.port == constants.DefaultContainerPort {
			setup.port = svc.Port
			ui.Info("Auto-discovered port: %d", svc.Port)
		}
	}

	var selectedService *site.ServiceInfo

	// If --service flag provided, find the matching service
	if addFlags.service != "" {
		for i, svc := range services {
			if svc.ContainerName == addFlags.service || svc.ServiceName == addFlags.service {
				selectedService = &services[i]
				break
			}
		}
		if selectedService == nil {
			return fmt.Errorf("service '%s' not found in compose file", addFlags.service)
		}
	} else if len(services) == 1 {
		// Single service - use it automatically
		selectedService = &services[0]
	} else {
		// Multiple services - prompt for selection
		options := make([]huh.Option[int], len(services))
		for i, svc := range services {
			label := svc.ContainerName
			if svc.ServiceName != svc.ContainerName {
				label = fmt.Sprintf("%s (service: %s)", svc.ContainerName, svc.ServiceName)
			}
			if len(svc.Profiles) > 0 {
				label = fmt.Sprintf("%s [%s]", label, strings.Join(svc.Profiles, ","))
			}
			// Show discovered port in selection
			if svc.Port > 0 {
				label = fmt.Sprintf("%s (port: %d)", label, svc.Port)
			}
			options[i] = huh.NewOption(label, i)
		}

		var selectedIdx int
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[int]().
					Title("Select container").
					Description("Which container should Traefik route to?").
					Options(options...).
					Value(&selectedIdx),
			),
		)
		if err := form.Run(); err != nil {
			return err
		}
		selectedService = &services[selectedIdx]
	}

	// Set the service info
	setServiceInfo(*selectedService)

	// Handle profile selection
	if len(selectedService.Profiles) == 1 {
		setup.profile = selectedService.Profiles[0]
	} else if len(selectedService.Profiles) > 1 {
		// Multiple profiles - prompt for selection
		if err := promptForProfile(setup, selectedService.Profiles); err != nil {
			return err
		}
	}

	return nil
}

// promptForProfile prompts the user to select a profile when multiple are available
func promptForProfile(setup *siteSetup, profiles []string) error {
	options := make([]huh.Option[string], len(profiles))
	for i, profile := range profiles {
		options[i] = huh.NewOption(profile, profile)
	}

	var selected string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select profile").
				Description("Which Docker Compose profile should be used?").
				Options(options...).
				Value(&selected),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}

	setup.profile = selected
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
// All config is stored in ~/.config/srv - no files are created in the project directory
func setupSiteFiles(cfg *config.Config, setup *siteSetup) error {
	if setup.isStatic {
		ui.Info("Configuring static site: %s", setup.siteName)
	} else {
		ui.Info("Configuring site: %s", setup.siteName)
	}

	// Determine site type
	siteType := site.SiteTypeCompose
	if setup.isStatic {
		siteType = site.SiteTypeStatic
	}

	// Write site metadata
	meta := site.SiteMetadata{
		Type:               siteType,
		Domain:             setup.domain,
		ProjectPath:        setup.sitePath,
		ServiceName:        setup.serviceName,
		ComposeServiceName: setup.composeServiceName,
		Profile:            setup.profile,
		Port:               setup.port,
		IsLocal:            setup.isLocal,
		NetworkName:        cfg.NetworkName,
		SPA:                setup.spa,
		Cache:              setup.cache,
		CORS:               setup.cors,
	}
	if err := site.WriteSiteMetadata(setup.siteName, meta); err != nil {
		return fmt.Errorf("failed to write site metadata: %w", err)
	}

	if setup.isStatic {
		// Static site: generate docker-compose.yml and nginx.conf in config dir
		if err := site.WriteStaticSiteConfig(setup.siteName, meta); err != nil {
			return fmt.Errorf("failed to write static site config: %w", err)
		}
	} else {
		// Docker-compose site: generate Traefik file provider config
		routeConfig := traefik.SiteRouteConfig{
			Name:        setup.siteName,
			Domain:      setup.domain,
			ServiceName: setup.serviceName,
			Port:        setup.port,
			IsLocal:     setup.isLocal,
		}
		if err := traefik.WriteSiteRouteConfig(cfg, routeConfig); err != nil {
			return fmt.Errorf("failed to write traefik config: %w", err)
		}
	}

	return nil
}

// finalizeSiteSetup handles SSL certs and optional start
func finalizeSiteSetup(cfg *config.Config, setup *siteSetup) error {
	// Generate SSL certificate for local domains
	if setup.isLocal {
		generateLocalCert(setup.siteName, setup.domain)
	}

	// Remove existing metadata if force
	if addFlags.force && site.Exists(setup.siteName) {
		if err := site.RemoveSiteMetadata(setup.siteName); err != nil {
			return fmt.Errorf("failed to remove existing site: %w", err)
		}
	}

	// Determine site type label
	var siteType string
	if setup.isStatic {
		siteType = "static"
	} else {
		siteType = "compose"
	}

	ui.Success("Site '%s' added successfully!", setup.siteName)
	ui.Dim("Domain: %s (%s, %s)", setup.domain, siteType, ui.Highlight(TypeLabel(setup.isLocal)))
	ui.Dim("Config: %s/sites/%s/ (no project files modified)", cfg.Root, setup.siteName)

	// Always start the site after adding
	return startSiteAfterAdd(cfg, setup)
}

// generateLocalCert generates SSL certificate for local domains and registers DNS
func generateLocalCert(siteName, domain string) {
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

	renewed, err := traefik.EnsureLocalCert(siteName, domain)
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
func renewLocalCertIfNeeded(siteName, domain string) {
	cert := traefik.GetLocalCertInfo(siteName, domain)
	if !cert.Exists || cert.IsExpired || cert.DaysLeft <= traefik.RenewThresholdDays {
		if cert.IsExpired {
			ui.Dim("Renewing expired SSL certificate for %s...", domain)
		} else if cert.Exists && cert.DaysLeft <= traefik.RenewThresholdDays {
			ui.Dim("Renewing SSL certificate for %s (expires in %d days)...", domain, cert.DaysLeft)
		}

		if err := traefik.GenerateLocalCert(siteName, domain); err != nil {
			ui.Warn("Warning: Failed to renew certificate: %v", err)
			return
		}

		if err := traefik.UpdateDynamicConfig(); err != nil {
			ui.Warn("Warning: Failed to update Traefik config: %v", err)
		}
	}
}

// startSiteAfterAdd starts the site after adding
func startSiteAfterAdd(cfg *config.Config, setup *siteSetup) error {
	ui.Blank()
	if setup.profile != "" {
		ui.Info("Starting site with profile '%s'...", setup.profile)
	} else {
		ui.Info("Starting site...")
	}

	// Determine the compose directory
	var composeDir string
	if setup.isStatic {
		// Static sites have their compose file in the srv config directory
		composeDir = site.SiteConfigDir(cfg, setup.siteName)
	} else {
		// Compose sites run from the project directory
		composeDir = setup.sitePath
	}

	if err := docker.ComposeUpWithProfile(composeDir, setup.profile); err != nil {
		return fmt.Errorf("failed to start site: %w", err)
	}

	// For compose sites, connect service to traefik network
	if !setup.isStatic && setup.composeServiceName != "" {
		if err := docker.ConnectServiceToNetwork(setup.sitePath, setup.composeServiceName, cfg.NetworkName); err != nil {
			if errors.Is(err, docker.ErrServiceNotRunning) {
				ui.Dim("Service '%s' not running (may use Docker Compose profiles)", setup.composeServiceName)
				ui.Dim("Network connection will happen when you start with your profile")
			} else {
				ui.Warn("Warning: Could not connect to traefik network: %v", err)
				ui.Dim("Run manually: docker network connect %s <container_name>", cfg.NetworkName)
			}
		}
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
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			cmd.Help()
			os.Exit(1)
		}
		return nil
	},
	RunE: runRemove,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	removeCmd.GroupID = GroupSites
	RootCmd.AddCommand(removeCmd)
}

func runRemove(cmd *cobra.Command, args []string) error {
	siteName := args[0]

	s, err := site.Get(siteName)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Stop containers if not broken
	if !s.IsBroken {
		ui.Info("Stopping containers...")
		// Use ComposeDir for docker operations
		if err := docker.ComposeDown(s.ComposeDir); err != nil {
			ui.Warn("Warning: Failed to stop containers: %v", err)
		}

		// Remove Traefik file provider config for compose sites
		if s.Type == site.SiteTypeCompose {
			if err := traefik.RemoveSiteRouteConfig(cfg, siteName); err != nil {
				ui.Warn("Warning: Could not remove traefik config: %v", err)
			} else {
				ui.Dim("Removed traefik config for %s", siteName)
			}
		}
	}

	// Remove SSL certificate and DNS for local domains
	if s.IsLocal && s.Domain != "" {
		if err := traefik.RemoveLocalCerts(siteName, s.Domain); err != nil {
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

	// Remove site metadata (includes docker-compose.yml and nginx.conf for static sites)
	if err := site.RemoveSiteMetadata(siteName); err != nil {
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
	Short:   "List all sites",
	RunE:    runList,
}

func init() {
	listCmd.GroupID = GroupSites
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
	headers := []string{"NAME", "DOMAIN", "PATH", "MODE", "SSL", "STATUS"}
	rows := make([][]string, 0, len(sites))

	for _, s := range sites {
		status := s.Status
		if s.IsBroken {
			status = constants.StatusBroken
		}

		// Determine mode (local dev vs production)
		mode := getModeLabel(s)

		// Determine SSL status
		sslStatus := getSSLStatus(s)

		// Show directory path (or placeholder if broken)
		dir := s.Dir
		if s.IsBroken {
			dir = ui.DimText("-")
		}

		rows = append(rows, []string{
			s.Name,
			s.Domain,
			dir,
			mode,
			sslStatus,
			ui.StatusColor(status),
		})
	}

	ui.PrintTable(headers, rows)
	return nil
}

// getModeLabel returns a formatted mode label for a site
func getModeLabel(s site.Site) string {
	if s.IsBroken {
		return ui.DimText("-")
	}

	// Show site type and SSL mode
	var typeLabel string
	if s.Type == site.SiteTypeStatic {
		typeLabel = "static"
	} else {
		typeLabel = "compose"
	}

	return typeLabel + "/" + ui.TypeColor(s.IsLocal)
}

// getSSLStatus returns a formatted SSL status string for a site
func getSSLStatus(s site.Site) string {
	if s.IsBroken {
		return ui.DimText("-")
	}

	if s.IsLocal {
		// Local site - check mkcert certificate
		cert := traefik.GetLocalCertInfo(s.Name, s.Domain)
		if !cert.Exists {
			return ui.StatusColor("missing")
		}
		if cert.IsExpired {
			return ui.StatusColor("expired")
		}
		if cert.DaysLeft <= constants.CertExpiryWarningDays {
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
	Use:   "info SITE",
	Short: "Show site info",
	Long: `Display detailed information about a site including:
  - Site name and path
  - Domain and type (local/production)
  - Container status
  - SSL certificate status (for local sites)`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			cmd.Help()
			os.Exit(1)
		}
		return nil
	},
	RunE: runInfo,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	infoCmd.GroupID = GroupSites
	RootCmd.AddCommand(infoCmd)
}

func runInfo(cmd *cobra.Command, args []string) error {
	s, err := site.Get(args[0])
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
	ui.Print("  SSL:     %s", ui.TypeColor(s.IsLocal))

	// Site type info
	if s.Type == site.SiteTypeStatic {
		ui.Print("  Type:    %s", "static (nginx)")
	} else {
		ui.Print("  Type:    %s", "compose")
		if s.ServiceName != "" {
			ui.Print("  Service: %s", s.ServiceName)
		}
		if s.Port != 0 {
			ui.Print("  Port:    %d", s.Port)
		}
	}

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
	if s.Status == constants.StatusRunning && s.Domain != "" {
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
			} else if cert.DaysLeft <= constants.CertExpiryWarningDays {
				ui.Print("  Status:  %s (%d days left)", ui.StatusColor("expiring"), cert.DaysLeft)
			} else {
				ui.Print("  Status:  %s (%d days left)", ui.StatusColor("valid"), cert.DaysLeft)
			}

			ui.Print("  Expires: %s", cert.ExpiresAt.Format(constants.DateFormat))
			return
		}
	}

	ui.Bold("SSL Certificate")
	ui.Dim("  No certificate found for %s", domain)
	ui.IndentedDim(1, "Certificate will be generated on 'srv start'")
}

// =============================================================================
// start command
// =============================================================================

var startFlags struct {
	all bool
}

var startCmd = &cobra.Command{
	Use:   "start SITE",
	Short: "Start a site",
	Long: `Start a site's containers.

Use --all to start all registered sites in parallel.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 && !startFlags.all {
			cmd.Help()
			os.Exit(1)
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
	startCmd.GroupID = GroupSites
	RootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	if startFlags.all {
		return startAllSites()
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
		renewLocalCertIfNeeded(s.Name, s.Domain)
	}

	ui.Info("Starting %s...", s.Name)
	// Use ComposeDir which is set correctly for both static and compose sites
	if err := docker.ComposeUpWithProfile(s.ComposeDir, s.Profile); err != nil {
		return fmt.Errorf("failed to start site: %w", err)
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
				ui.Warn("Warning: Could not connect to traefik network: %v", err)
				ui.Dim("Run manually: docker network connect %s <container_name>", cfg.NetworkName)
			}
		}
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

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Renew any expiring local certs before starting
	for _, s := range sites {
		if s.IsLocal && s.Domain != "" && !s.IsBroken {
			renewLocalCertIfNeeded(s.Name, s.Domain)
		}
	}

	ui.Info("Starting %d site(s)...", len(sites))
	runBatchSiteOperation(sites, "start", func(s *site.Site) error {
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
	Use:   "stop SITE",
	Short: "Stop a site",
	Long: `Stop a site's containers.

Use --all to stop all registered sites in parallel.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 && !stopFlags.all {
			cmd.Help()
			os.Exit(1)
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

	s, err := site.Get(args[0])
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
	runBatchSiteOperation(sites, "stop", func(s *site.Site) error {
		return docker.ComposeStop(s.ComposeDir)
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
	Use:   "restart SITE",
	Short: "Restart a site",
	Long: `Restart a site's containers.

Use --all to restart all registered sites in parallel.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 && !restartFlags.all {
			cmd.Help()
			os.Exit(1)
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
	restartCmd.GroupID = GroupSites
	RootCmd.AddCommand(restartCmd)
}

func runRestart(cmd *cobra.Command, args []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	if restartFlags.all {
		return restartAllSites()
	}

	s, err := site.Get(args[0])
	if err != nil {
		return err
	}

	if s.IsBroken {
		return fmt.Errorf("site '%s' is broken (target directory missing)", s.Name)
	}

	ui.Info("Restarting %s...", s.Name)
	if err := docker.ComposeRestart(s.ComposeDir); err != nil {
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
		return docker.ComposeRestart(s.ComposeDir)
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
	workers := min(constants.MaxWorkers, len(validSites))

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
	Short: "Show site logs",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			cmd.Help()
			os.Exit(1)
		}
		return nil
	},
	RunE: runLogs,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFlags.follow, "follow", "f", false, "Follow log output")
	logsCmd.Flags().StringVar(&logsFlags.tail, "tail", "", "Number of lines to show from the end")
	logsCmd.Flags().StringVar(&logsFlags.since, "since", "", "Show logs since timestamp (e.g., 10m, 1h)")
	logsCmd.GroupID = GroupSites
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

	return docker.Compose(s.ComposeDir, composeArgs...)
}
