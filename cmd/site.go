package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/pool"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

// =============================================================================
// add command
// =============================================================================

var addFlags struct {
	domain         string
	aliases        []string
	port           int
	name           string
	service        string
	local          bool
	wildcard       bool
	internalHTTP   bool
	force          bool
	skipValidation bool
	typeOverride   string // Force site type: php/node/ruby/python/dockerfile/static/compose
	// Static site options
	spa   bool
	cache bool
	cors  bool
	// Limits
	maxBody        string
	readTimeout    string
	sendTimeout    string
	connectTimeout string
	// PHP site options
	phpVersion    string
	documentRoot  string
	phpExtensions string
	// Node.js site options
	nodeVersion string
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
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			_ = cmd.Help()
			return ui.UsageError("srv add PATH --domain DOMAIN", "a path to a directory is required")
		}
		if len(args) > 1 {
			return ui.UsageError("srv add PATH --domain DOMAIN", "too many arguments — expected a single directory path, got %d", len(args))
		}
		return nil
	},
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if addFlags.domain == "" {
			_ = cmd.Help()
			return ui.UsageError("srv add PATH --domain DOMAIN", "--domain is required (e.g. --domain myapp.test or --domain example.com)")
		}
		return nil
	},
	RunE: runAdd,
}

func init() {
	addCmd.Flags().StringVarP(&addFlags.domain, "domain", "d", "", "Domain/hostname (e.g., example.com or myapp.test)")
	addCmd.Flags().StringSliceVar(&addFlags.aliases, "alias", nil, "Additional hostname mapped to the same site (repeatable)")
	_ = addCmd.RegisterFlagCompletionFunc("alias", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveNoFileComp
	})
	addCmd.Flags().IntVarP(&addFlags.port, "port", "p", constants.DefaultContainerPort, "Container port")
	addCmd.Flags().StringVarP(&addFlags.name, "name", "n", "", "Site name (default: directory name)")
	addCmd.Flags().StringVar(&addFlags.service, "service", "", "Container name to route to")
	addCmd.Flags().BoolVarP(&addFlags.local, "local", "l", false, "Use local SSL via mkcert (otherwise Let's Encrypt)")
	addCmd.Flags().BoolVar(&addFlags.wildcard, "wildcard", false, "Also match one-level subdomains (e.g. *.foo.test); local sites only")
	addCmd.Flags().BoolVar(&addFlags.internalHTTP, "internal-http", false, "Expose the site on the internal plain-HTTP entrypoint (port 88) in addition to HTTPS")
	addCmd.Flags().BoolVarP(&addFlags.force, "force", "f", false, "Overwrite existing configuration")
	addCmd.Flags().BoolVar(&addFlags.skipValidation, "skip-validation", false, "Skip compose file validation")
	// Static site options
	addCmd.Flags().BoolVar(&addFlags.spa, "spa", true, "Enable SPA mode (fallback to index.html)")
	addCmd.Flags().BoolVar(&addFlags.cache, "cache", true, "Enable caching headers for static assets")
	addCmd.Flags().BoolVar(&addFlags.cors, "cors", false, "Enable CORS headers (allow all origins)")
	// Limits
	addCmd.Flags().StringVar(&addFlags.maxBody, "max-body", "", "Maximum request body size (e.g. 128M, 2G)")
	_ = addCmd.RegisterFlagCompletionFunc("max-body", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"1M", "10M", "100M", "128M", "1G", "2G"}, cobra.ShellCompDirectiveNoFileComp
	})
	addCmd.Flags().StringVar(&addFlags.readTimeout, "read-timeout", "", "Upstream read timeout (e.g. 30s, 300s)")
	addCmd.Flags().StringVar(&addFlags.sendTimeout, "send-timeout", "", "Upstream send timeout (e.g. 30s, 300s)")
	addCmd.Flags().StringVar(&addFlags.connectTimeout, "connect-timeout", "", "Upstream connect timeout (e.g. 5s)")
	for _, name := range []string{"read-timeout", "send-timeout", "connect-timeout"} {
		_ = addCmd.RegisterFlagCompletionFunc(name, func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return []string{"1s", "5s", "10s", "30s", "60s", "300s"}, cobra.ShellCompDirectiveNoFileComp
		})
	}
	// PHP site options
	addCmd.Flags().StringVar(&addFlags.phpVersion, "php-version", "", "PHP version (auto-detected; use 'latest' for newest)")
	addCmd.Flags().StringVar(&addFlags.documentRoot, "document-root", "", "Document root relative to project (auto-detected)")
	addCmd.Flags().StringVar(&addFlags.phpExtensions, "php-extensions", "", "PHP extensions: full list, or +ext/-ext to add/remove from defaults")
	_ = addCmd.RegisterFlagCompletionFunc("php-extensions", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return site.KnownPHPExtensions(), cobra.ShellCompDirectiveNoFileComp
	})
	// Node.js site options
	addCmd.Flags().StringVar(&addFlags.nodeVersion, "node-version", "", "Node.js version (auto-detected from .nvmrc / package.json; use 'lts' for latest LTS)")
	// Type override
	addCmd.Flags().StringVar(&addFlags.typeOverride, "type", "", "Force site type: php, node, ruby, python, dockerfile, static, compose")
	_ = addCmd.RegisterFlagCompletionFunc("type", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"php", "node", "ruby", "python", "dockerfile", "static", "compose"}, cobra.ShellCompDirectiveNoFileComp
	})
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

	if err := docker.EnsureInitialized(cfg.NetworkName); err != nil {
		return err
	}

	// Validate and resolve site setup
	setup, err := validateSiteSetup(args[0])
	if err != nil {
		return err
	}

	// Print detection summary before prompting
	ui.Dim("Detected: %s", detectionSummary(setup))

	// Prompt for any missing configuration
	if err := promptForMissingConfig(setup); err != nil {
		return err
	}

	// Validate inputs
	if err := validateSiteInputs(setup); err != nil {
		return err
	}

	if addFlags.force && site.Exists(setup.siteName) {
		// Back up old metadata so it can be restored if the new write fails.
		oldDir := site.SiteConfigDir(cfg, setup.siteName)
		backupDir := oldDir + ".bak"
		_ = os.RemoveAll(backupDir)
		if renameErr := os.Rename(oldDir, backupDir); renameErr != nil && !os.IsNotExist(renameErr) {
			return fmt.Errorf("failed to back up existing site metadata: %w", renameErr)
		}
		if err := setupSiteFiles(cfg, setup); err != nil {
			_ = os.RemoveAll(oldDir)
			_ = os.Rename(backupDir, oldDir)
			return err
		}
		_ = os.RemoveAll(backupDir)
	} else {
		if err := setupSiteFiles(cfg, setup); err != nil {
			return err
		}
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
	domain             string   // canonical/primary domain
	aliases            []string // extra hostnames mapped to the same site
	listeners          []string // extra Traefik entrypoints (e.g. "internal")
	limits             *site.Limits
	port               int
	isLocal            bool
	wildcard           bool // true if wildcard subdomain matching is enabled
	isStatic           bool // true if serving static files (no docker-compose.yml)
	isPHP              bool // true if serving a PHP/FPM site
	isNode             bool // true if serving a Node.js site
	isRuby             bool // true if serving a Ruby site
	isPython           bool // true if serving a Python site
	isDockerfile       bool // true if building from a bare Dockerfile
	phpInfo            *site.PHPSiteInfo
	nodeInfo           *site.NodeSiteInfo
	rubyInfo           *site.RubySiteInfo
	pythonInfo         *site.PythonSiteInfo
	dockerfileInfo     *site.DockerfileSiteInfo
	// Static site options
	spa   bool
	cache bool
	cors  bool
}

// validateSiteSetup validates the path and discovers compose file or PHP/Node project.
// Detection order (when --type is not specified):
//  1. docker-compose.yml present → compose site
//  2. composer.json present      → PHP site (with full metadata)
//  3. *.php / *.phtml present    → PHP site (raw, with defaults)
//  4. package.json / deno.json   → Node.js / Bun / Deno site
//  5. Gemfile present            → Ruby site
//  6. requirements.txt / pyproject.toml / Pipfile → Python site
//  7. Dockerfile present         → Dockerfile site
//  8. otherwise                  → static site
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

	// --type override: skip auto-detection and set type directly.
	if addFlags.typeOverride != "" {
		return applyTypeOverride(setup, sitePath, addFlags.typeOverride)
	}

	// 1. Try to find a compose file.
	composePath, err := site.FindComposeFile(sitePath)
	if err != nil && !site.IsNotFoundError(err) {
		return nil, fmt.Errorf("could not check for docker-compose file: %w", err)
	}
	if err == nil {
		setup.composePath = composePath
		return setup, nil
	}

	// 2. Try composer.json-based PHP detection.
	phpInfo, err := site.DetectPHPSite(sitePath)
	if err != nil {
		return nil, fmt.Errorf("could not check for PHP project: %w", err)
	}
	if phpInfo != nil {
		setup.isPHP = true
		setup.phpInfo = phpInfo
		return setup, nil
	}

	// 3. Try raw PHP file detection.
	isRawPHP, err := site.DetectRawPHPSite(sitePath)
	if err != nil {
		return nil, fmt.Errorf("could not check for PHP files: %w", err)
	}
	if isRawPHP {
		setup.isPHP = true
		setup.phpInfo = site.RawPHPDefaults()
		return setup, nil
	}

	// 4. Try Node.js / Bun / Deno detection.
	nodeInfo, err := site.DetectNodeSite(sitePath)
	if err != nil {
		return nil, fmt.Errorf("could not check for Node.js project: %w", err)
	}
	if nodeInfo != nil {
		setup.isNode = true
		setup.nodeInfo = nodeInfo
		return setup, nil
	}

	// 5. Try Ruby detection (Gemfile).
	rubyInfo, err := site.DetectRubySite(sitePath)
	if err != nil {
		return nil, fmt.Errorf("could not check for Ruby project: %w", err)
	}
	if rubyInfo != nil {
		setup.isRuby = true
		setup.rubyInfo = rubyInfo
		return setup, nil
	}

	// 6. Try Python detection.
	pythonInfo, err := site.DetectPythonSite(sitePath)
	if err != nil {
		return nil, fmt.Errorf("could not check for Python project: %w", err)
	}
	if pythonInfo != nil {
		setup.isPython = true
		setup.pythonInfo = pythonInfo
		return setup, nil
	}

	// 7. Try bare Dockerfile detection.
	dockerfileInfo, err := site.DetectDockerfileSite(sitePath)
	if err != nil {
		return nil, fmt.Errorf("could not check for Dockerfile: %w", err)
	}
	if dockerfileInfo != nil {
		setup.isDockerfile = true
		setup.dockerfileInfo = dockerfileInfo
		return setup, nil
	}

	// 8. Fall back to static site.
	setup.isStatic = true
	return setup, nil
}

// applyTypeOverride forces a specific site type, running detection only for that type.
func applyTypeOverride(setup *siteSetup, sitePath, typeStr string) (*siteSetup, error) {
	switch strings.ToLower(typeStr) {
	case "php":
		phpInfo, err := site.DetectPHPSite(sitePath)
		if err != nil || phpInfo == nil {
			phpInfo = site.RawPHPDefaults()
		}
		setup.isPHP = true
		setup.phpInfo = phpInfo
	case "node":
		nodeInfo, err := site.DetectNodeSite(sitePath)
		if err != nil || nodeInfo == nil {
			nodeInfo = site.NodeDefaults()
		}
		setup.isNode = true
		setup.nodeInfo = nodeInfo
	case "ruby":
		rubyInfo, err := site.DetectRubySite(sitePath)
		if err != nil || rubyInfo == nil {
			rubyInfo = &site.RubySiteInfo{
				RubyVersion: constants.RubyVersionLatest,
				Framework:   constants.RubyFrameworkGeneric,
				Port:        constants.RubyDefaultPort,
				StartCmd:    "sh -c 'bundle install && bundle exec ruby app.rb'",
			}
		}
		setup.isRuby = true
		setup.rubyInfo = rubyInfo
	case "python":
		pythonInfo, err := site.DetectPythonSite(sitePath)
		if err != nil || pythonInfo == nil {
			pythonInfo = &site.PythonSiteInfo{
				PythonVersion: constants.PythonVersionLatest,
				Framework:     constants.PythonFrameworkGeneric,
				Port:          constants.PythonDefaultPort,
				StartCmd:      "sh -c 'pip install -r requirements.txt && python app.py'",
			}
		}
		setup.isPython = true
		setup.pythonInfo = pythonInfo
	case "dockerfile":
		dockerfileInfo, err := site.DetectDockerfileSite(sitePath)
		if err != nil || dockerfileInfo == nil {
			dockerfileInfo = &site.DockerfileSiteInfo{Port: constants.DockerfileDefaultPort}
		}
		setup.isDockerfile = true
		setup.dockerfileInfo = dockerfileInfo
	case "static":
		setup.isStatic = true
	case "compose":
		composePath, err := site.FindComposeFile(sitePath)
		if err != nil {
			return nil, fmt.Errorf("no docker-compose.yml found (required for --type compose)")
		}
		setup.composePath = composePath
	default:
		return nil, fmt.Errorf("unknown site type %q — valid types: php, node, ruby, python, dockerfile, static, compose", typeStr)
	}
	return setup, nil
}

// promptForMissingConfig prompts user for any missing configuration
func promptForMissingConfig(setup *siteSetup) error {
	// Get service name (only for compose sites)
	if !setup.isStatic && !setup.isPHP && !setup.isNode && !setup.isRuby && !setup.isPython && !setup.isDockerfile {
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
	setup.wildcard = addFlags.wildcard
	if setup.wildcard && !setup.isLocal {
		return fmt.Errorf("--wildcard requires --local (Let's Encrypt cannot issue local wildcard certs)")
	}

	// Aliases: validate each, ensure no clash with the canonical domain, dedupe.
	if aliases, err := normalizeAliases(setup.domain, addFlags.aliases); err != nil {
		return err
	} else {
		setup.aliases = aliases
	}

	// Limits: collect any user-supplied overrides. Empty values are omitted.
	if l := limitsFromFlags(); l != nil {
		setup.limits = l
	}

	if addFlags.internalHTTP {
		setup.listeners = append(setup.listeners, constants.ListenerInternal)
	}

	// Static site options
	setup.spa = addFlags.spa
	setup.cache = addFlags.cache
	setup.cors = addFlags.cors

	// PHP site: apply flag overrides on top of auto-detected values.
	if setup.isPHP && setup.phpInfo != nil {
		if addFlags.phpVersion != "" {
			setup.phpInfo.PHPVersion = addFlags.phpVersion
		}
		if addFlags.documentRoot != "" {
			setup.phpInfo.DocumentRoot = addFlags.documentRoot
		}
		if addFlags.phpExtensions != "" {
			setup.phpInfo.Extensions = site.ParseExtensionOverrides(
				addFlags.phpExtensions,
				setup.phpInfo.Extensions,
			)
		}
	}

	// Node.js site: apply flag overrides on top of auto-detected values.
	if setup.isNode && setup.nodeInfo != nil {
		if addFlags.nodeVersion != "" {
			setup.nodeInfo.NodeVersion = addFlags.nodeVersion
		}
		// If the user explicitly set --port, use it; otherwise keep the detected port.
		if addFlags.port != constants.DefaultContainerPort {
			setup.nodeInfo.Port = addFlags.port
		}
	}

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
	setServiceInfo := func(svc site.ServiceInfo) error {
		if err := ValidateContainerName(svc.ContainerName); err != nil {
			return fmt.Errorf("compose container name: %w", err)
		}
		if err := ValidateContainerName(svc.ServiceName); err != nil {
			return fmt.Errorf("compose service name: %w", err)
		}
		setup.serviceName = svc.ContainerName
		setup.composeServiceName = svc.ServiceName
		// Use discovered port if user didn't explicitly set one
		if svc.Port > 0 && setup.port == constants.DefaultContainerPort {
			setup.port = svc.Port
			ui.Info("Auto-discovered port: %d", svc.Port)
		}
		return nil
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
	if err := setServiceInfo(*selectedService); err != nil {
		return err
	}

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
	switch {
	case setup.isPHP:
		ui.Info("Configuring PHP site: %s", setup.siteName)
	case setup.isNode:
		ui.Info("Configuring Node.js site: %s", setup.siteName)
	case setup.isRuby:
		ui.Info("Configuring Ruby site: %s", setup.siteName)
	case setup.isPython:
		ui.Info("Configuring Python site: %s", setup.siteName)
	case setup.isDockerfile:
		ui.Info("Configuring Dockerfile site: %s", setup.siteName)
	case setup.isStatic:
		ui.Info("Configuring static site: %s", setup.siteName)
	default:
		ui.Info("Configuring site: %s", setup.siteName)
	}

	// Determine site type
	siteType := site.SiteTypeCompose
	switch {
	case setup.isPHP:
		siteType = site.SiteTypePHP
	case setup.isNode:
		siteType = site.SiteTypeNode
	case setup.isRuby:
		siteType = site.SiteTypeRuby
	case setup.isPython:
		siteType = site.SiteTypePython
	case setup.isDockerfile:
		siteType = site.SiteTypeDockerfile
	case setup.isStatic:
		siteType = site.SiteTypeStatic
	}

	// Determine canonical port for routing metadata.
	port := setup.port
	switch {
	case setup.isNode && setup.nodeInfo != nil:
		port = setup.nodeInfo.Port
	case setup.isRuby && setup.rubyInfo != nil:
		port = setup.rubyInfo.Port
	case setup.isPython && setup.pythonInfo != nil:
		port = setup.pythonInfo.Port
	case setup.isDockerfile && setup.dockerfileInfo != nil:
		port = setup.dockerfileInfo.Port
	}

	// Build base metadata.
	meta := site.SiteMetadata{
		Type:               siteType,
		Domains:            setup.allDomains(),
		ProjectPath:        setup.sitePath,
		ServiceName:        setup.serviceName,
		ComposeServiceName: setup.composeServiceName,
		Profile:            setup.profile,
		Port:               port,
		IsLocal:            setup.isLocal,
		Wildcard:           setup.wildcard,
		NetworkName:        cfg.NetworkName,
		Listeners:          setup.listeners,
		Limits:             setup.limits,
		SPA:                setup.spa,
		Cache:              setup.cache,
		CORS:               setup.cors,
	}

	// Add PHP-specific fields to metadata.
	if setup.isPHP && setup.phpInfo != nil {
		meta.PHPVersion = setup.phpInfo.PHPVersion
		meta.PHPExtensions = setup.phpInfo.Extensions
		meta.PHPFramework = setup.phpInfo.Framework
		meta.DocumentRoot = setup.phpInfo.DocumentRoot
	}

	// Add Node.js-specific fields to metadata.
	if setup.isNode && setup.nodeInfo != nil {
		meta.NodeRuntime = setup.nodeInfo.Runtime
		meta.NodePackageManager = setup.nodeInfo.PackageManager
		meta.NodeVersion = setup.nodeInfo.NodeVersion
		meta.NodeFramework = setup.nodeInfo.Framework
		meta.NodeStartCmd = setup.nodeInfo.StartCmd
		meta.ServiceName = "srv-" + setup.siteName + "-node"
	}

	// Add Ruby-specific fields to metadata.
	if setup.isRuby && setup.rubyInfo != nil {
		meta.RubyVersion = setup.rubyInfo.RubyVersion
		meta.RubyFramework = setup.rubyInfo.Framework
		meta.RubyStartCmd = setup.rubyInfo.StartCmd
		meta.ServiceName = "srv-" + setup.siteName + "-app"
	}

	// Add Python-specific fields to metadata.
	if setup.isPython && setup.pythonInfo != nil {
		meta.PythonVersion = setup.pythonInfo.PythonVersion
		meta.PythonFramework = setup.pythonInfo.Framework
		meta.PythonStartCmd = setup.pythonInfo.StartCmd
		meta.ServiceName = "srv-" + setup.siteName + "-app"
	}

	// Add Dockerfile-specific fields to metadata.
	if setup.isDockerfile && setup.dockerfileInfo != nil {
		meta.DockerfilePort = setup.dockerfileInfo.Port
		meta.ServiceName = "srv-" + setup.siteName + "-app"
	}

	if err := site.WriteSiteMetadata(setup.siteName, meta); err != nil {
		return fmt.Errorf("failed to write site metadata: %w", err)
	}

	switch {
	case setup.isPHP:
		// PHP site: generate Dockerfile, nginx.conf, and docker-compose.yml.
		if err := site.WritePHPSiteConfig(setup.siteName, meta, setup.phpInfo, addFlags.force); err != nil {
			return fmt.Errorf("failed to write PHP site config: %w", err)
		}
	case setup.isNode:
		// Node.js site: generate docker-compose.yml.
		if err := site.WriteNodeSiteConfig(setup.siteName, meta, setup.nodeInfo, addFlags.force); err != nil {
			return fmt.Errorf("failed to write Node site config: %w", err)
		}
	case setup.isRuby:
		// Ruby site: generate docker-compose.yml.
		if err := site.WriteRubySiteConfig(setup.siteName, meta, setup.rubyInfo, addFlags.force); err != nil {
			return fmt.Errorf("failed to write Ruby site config: %w", err)
		}
	case setup.isPython:
		// Python site: generate docker-compose.yml.
		if err := site.WritePythonSiteConfig(setup.siteName, meta, setup.pythonInfo, addFlags.force); err != nil {
			return fmt.Errorf("failed to write Python site config: %w", err)
		}
	case setup.isDockerfile:
		// Dockerfile site: generate docker-compose.yml that builds from project Dockerfile.
		if err := site.WriteDockerfileSiteConfig(setup.siteName, meta, setup.dockerfileInfo, addFlags.force); err != nil {
			return fmt.Errorf("failed to write Dockerfile site config: %w", err)
		}
	case setup.isStatic:
		// Static site: generate docker-compose.yml and nginx.conf in config dir.
		if err := site.WriteStaticSiteConfig(setup.siteName, meta, addFlags.force); err != nil {
			return fmt.Errorf("failed to write static site config: %w", err)
		}
	default:
		// Docker-compose site: generate Traefik file provider config.
		routeConfig := traefik.SiteRouteConfig{
			Name:        setup.siteName,
			Domains:     setup.allDomains(),
			ServiceName: setup.serviceName,
			Port:        setup.port,
			IsLocal:     setup.isLocal,
			Wildcard:    setup.wildcard,
			Listeners:   meta.Listeners,
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
		generateLocalCert(setup.siteName, setup.allDomains(), setup.wildcard)
	}

	// Determine site type label
	var siteType string
	switch {
	case setup.isPHP:
		siteType = "php"
	case setup.isNode:
		siteType = "node"
	case setup.isRuby:
		siteType = "ruby"
	case setup.isPython:
		siteType = "python"
	case setup.isDockerfile:
		siteType = "dockerfile"
	case setup.isStatic:
		siteType = "static"
	default:
		siteType = "compose"
	}

	ui.Success("Site '%s' added successfully!", setup.siteName)
	ui.Dim("Domain: %s (%s, %s)", setup.domain, siteType, ui.Highlight(TypeLabel(setup.isLocal)))
	ui.Dim("Config: %s/sites/%s/ (no project files modified)", cfg.Root, setup.siteName)

	if setup.isPHP && setup.phpInfo != nil && setup.phpInfo.Framework == constants.PHPFrameworkLaravel {
		ui.Blank()
		ui.Dim("Laravel: ensure storage and bootstrap/cache are writable:")
		ui.Dim("  chmod -R 777 %s/storage %s/bootstrap/cache", setup.sitePath, setup.sitePath)
	}

	// Always start the site after adding
	return startSiteAfterAdd(cfg, setup)
}

// normalizeAliases validates the supplied alias list, lowercases entries, drops
// duplicates, and rejects any alias equal to the canonical domain.
func normalizeAliases(canonical string, aliases []string) ([]string, error) {
	canonical = strings.ToLower(strings.TrimSpace(canonical))
	seen := map[string]bool{canonical: true}
	out := make([]string, 0, len(aliases))
	for _, raw := range aliases {
		a := strings.ToLower(strings.TrimSpace(raw))
		if a == "" {
			continue
		}
		if err := ValidateDomain(a); err != nil {
			return nil, fmt.Errorf("invalid alias %q: %w", raw, err)
		}
		if seen[a] {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	return out, nil
}

// limitsFromFlags returns a *site.Limits populated from any non-empty
// --max-body / --*-timeout flags, or nil if none were supplied.
func limitsFromFlags() *site.Limits {
	if addFlags.maxBody == "" && addFlags.readTimeout == "" && addFlags.sendTimeout == "" && addFlags.connectTimeout == "" {
		return nil
	}
	return &site.Limits{
		MaxBody:        addFlags.maxBody,
		ReadTimeout:    addFlags.readTimeout,
		SendTimeout:    addFlags.sendTimeout,
		ConnectTimeout: addFlags.connectTimeout,
	}
}

// allDomains returns the canonical domain followed by any aliases.
func (s *siteSetup) allDomains() []string {
	out := make([]string, 0, 1+len(s.aliases))
	if s.domain != "" {
		out = append(out, s.domain)
	}
	out = append(out, s.aliases...)
	return out
}

// generateLocalCert generates SSL certificate for local domains and registers DNS
// for every supplied domain. DNS registration always runs regardless of whether
// mkcert is available — TLS and DNS are independent concerns.
func generateLocalCert(siteName string, domains []string, wildcard bool) {
	if len(domains) == 0 {
		return
	}
	primary := domains[0]

	// Always register DNS — this must happen even if mkcert is missing.
	for _, d := range domains {
		if err := traefik.RegisterLocalDomain(d, wildcard); err != nil {
			ui.Warn("Failed to register DNS for %s: %v", d, err)
		}
	}
	ui.Dim("If a domain doesn't resolve, clear your browser DNS cache:")
	ui.Dim("  Chrome: chrome://net-internals/#dns  →  Clear host cache")
	ui.Dim("  Firefox: about:networking#dns  →  Clear DNS Cache")

	if err := traefik.CheckMkcert(); err != nil {
		ui.Warn("%v", err)
		ui.Dim("Local HTTPS will not work without mkcert")
		return
	}

	// Auto-install CA if not already installed
	if !traefik.IsCAInstalled() {
		ui.Dim("Installing mkcert CA...")
		res, err := traefik.InstallCA()
		if err != nil {
			ui.Warn("Failed to install mkcert CA: %v", err)
			ui.Dim("Local HTTPS may not work in browsers")
		} else {
			reportCAInstall(res, false)
		}
	}

	renewed, err := traefik.EnsureLocalCert(siteName, domains, wildcard)
	if err != nil {
		ui.Warn("Failed to generate certificate: %v", err)
		return
	}

	if renewed {
		ui.Dim("Generated SSL certificate for %s", primary)
		if err := traefik.UpdateDynamicConfig(); err != nil {
			ui.Warn("Failed to update Traefik config: %v", err)
		}
	}
}

// renewLocalCertIfNeeded checks if a local cert needs renewal and renews it.
// The cert is named after the primary (first) domain on disk.
func renewLocalCertIfNeeded(siteName string, domains []string, wildcard bool) {
	if len(domains) == 0 {
		return
	}
	primary := domains[0]
	cert := traefik.GetLocalCertInfo(siteName, primary)
	if !cert.Exists || cert.IsExpired || cert.DaysLeft <= traefik.RenewThresholdDays {
		if cert.IsExpired {
			ui.Dim("Renewing expired SSL certificate for %s...", primary)
		} else if cert.Exists && cert.DaysLeft <= traefik.RenewThresholdDays {
			ui.Dim("Renewing SSL certificate for %s (expires in %d days)...", primary, cert.DaysLeft)
		}

		if err := traefik.GenerateLocalCert(siteName, domains, wildcard); err != nil {
			ui.Warn("Failed to renew certificate: %v", err)
			return
		}

		if err := traefik.UpdateDynamicConfig(); err != nil {
			ui.Warn("Failed to update Traefik config: %v", err)
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
	if setup.isStatic || setup.isPHP || setup.isNode || setup.isRuby || setup.isPython || setup.isDockerfile {
		// srv-managed sites have their compose file in the srv config directory
		composeDir = site.SiteConfigDir(cfg, setup.siteName)
	} else {
		// Compose sites run from the project directory
		composeDir = setup.sitePath
	}

	if err := docker.ComposeUpWithProfile(composeDir, setup.profile); err != nil {
		return fmt.Errorf("failed to start site: %w", err)
	}

	// For PHP sites: run composer install automatically if vendor/ is absent.
	// The project is bind-mounted so vendor/ written inside the container is
	// immediately visible on the host as well.
	if setup.isPHP {
		vendorDir := setup.sitePath + "/vendor"
		if _, err := os.Stat(vendorDir); os.IsNotExist(err) {
			containerName := phpFPMContainerForSite(setup.siteName)
			ui.Info("Running composer install...")
			workDir := "/var/www/" + setup.siteName
			if err := docker.ExecNonInteractiveAt(containerName, workDir, "composer", "install", "--no-interaction", "--prefer-dist"); err != nil {
				ui.Warn("composer install failed: %v", err)
				ui.Dim("Run manually: srv site shell %s", setup.siteName)
			}
		}
	}

	// For compose sites, connect service to traefik network.
	// Static, PHP, and Node sites manage network membership via compose labels.
	if !setup.isStatic && !setup.isPHP && !setup.isNode && !setup.isRuby && !setup.isPython && !setup.isDockerfile && setup.composeServiceName != "" {
		if err := docker.ConnectServiceToNetwork(setup.sitePath, setup.composeServiceName, cfg.NetworkName); err != nil {
			if errors.Is(err, docker.ErrServiceNotRunning) {
				ui.Dim("Service '%s' not running (may use Docker Compose profiles)", setup.composeServiceName)
				ui.Dim("Network connection will happen when you start with your profile")
			} else {
				ui.Warn("Could not connect to traefik network: %v", err)
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
		if len(args) == 0 {
			_ = cmd.Help()
			return ui.UsageError("srv remove SITE", "a site name is required")
		}
		if len(args) > 1 {
			return ui.UsageError("srv remove SITE", "too many arguments — expected a single site name, got %d", len(args))
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

	s, err := site.GetByName(siteName)
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
			ui.Warn("Failed to stop containers: %v", err)
		}

		// Remove Traefik file provider config for compose sites
		if s.Type == site.SiteTypeCompose {
			if err := traefik.RemoveSiteRouteConfig(cfg, siteName); err != nil {
				ui.Warn("Could not remove traefik config: %v", err)
			} else {
				ui.Dim("Removed traefik config for %s", siteName)
			}
		}

		// Remove per-site extra-routes file if present.
		if err := traefik.RemoveRoutesConfig(cfg, siteName); err != nil {
			ui.Warn("Could not remove routes config: %v", err)
		}

		// Drop this site from its shared FPM pool. Tears the pool down if no
		// members remain.
		if s.Type == site.SiteTypePHP {
			if err := site.RemoveSiteFromPool(siteName); err != nil {
				ui.Warn("Could not refresh FPM pool: %v", err)
			}
		}
	}

	// Remove SSL certificate and DNS for local domains
	if s.IsLocal && len(s.Domains) > 0 {
		primary := s.Domains[0]
		if err := traefik.RemoveLocalCerts(siteName, primary); err != nil {
			ui.Warn("Failed to remove certificate: %v", err)
		}
		// Update Traefik dynamic config
		if err := traefik.UpdateDynamicConfig(); err != nil {
			ui.Warn("Failed to update Traefik config: %v", err)
		}
		// Unregister all domains from local DNS
		for _, d := range s.Domains {
			if err := traefik.UnregisterLocalDomain(d); err != nil {
				ui.Warn("Failed to unregister DNS for %s: %v", d, err)
			}
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
	headers := []string{"NAME", "DOMAIN", "TARGET", "TYPE", "SSL", "STATUS"}
	rows := make([][]string, 0, len(sites))

	for _, s := range sites {
		status := s.Status
		if s.IsBroken {
			status = constants.StatusBroken
		}

		// Determine SSL status
		sslStatus := getSSLStatus(s)

		// Show directory path as target (or placeholder if broken)
		target := s.Dir
		if s.IsBroken {
			target = ui.DimText("-")
		}

		rows = append(rows, []string{
			s.Name,
			formatDomainsForList(s.Domains),
			target,
			getSiteTypeLabel(s),
			sslStatus,
			ui.StatusColor(status),
		})
	}

	ui.PrintTable(headers, rows)
	return nil
}

// formatDomainsForList renders a site's domains for the `srv list` table.
// Returns the primary alone if only one is set; otherwise primary plus a
// "+N" indicator so the table stays narrow.
func formatDomainsForList(domains []string) string {
	switch len(domains) {
	case 0:
		return ""
	case 1:
		return domains[0]
	default:
		return fmt.Sprintf("%s (+%d)", domains[0], len(domains)-1)
	}
}

// getSiteTypeLabel returns the site type label for the list view.
func getSiteTypeLabel(s site.Site) string {
	if s.IsBroken {
		return ui.DimText("-")
	}
	switch s.Type {
	case site.SiteTypeStatic:
		return "static"
	case site.SiteTypePHP:
		return "php"
	case site.SiteTypeNode:
		return "node"
	case site.SiteTypeRuby:
		return "ruby"
	case site.SiteTypePython:
		return "python"
	case site.SiteTypeDockerfile:
		return "dockerfile"
	default:
		return "compose"
	}
}

// getSSLStatus returns a formatted SSL status string for a site
func getSSLStatus(s site.Site) string {
	if s.IsBroken {
		return ui.DimText("-")
	}

	if s.IsLocal {
		// Local site - check mkcert certificate (named after the primary domain)
		cert := traefik.GetLocalCertInfo(s.Name, s.Domain())
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
		if len(args) == 0 {
			_ = cmd.Help()
			return ui.UsageError("srv info SITE", "a site name is required")
		}
		if len(args) > 1 {
			return ui.UsageError("srv info SITE", "too many arguments — expected a single site name, got %d", len(args))
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
	s, err := site.GetByName(args[0])
	if err != nil {
		return err
	}

	ui.Blank()
	ui.Bold("Site: %s", s.Name)
	ui.Blank()

	// Basic info
	ui.Print("  Path:    %s", s.Dir)
	if len(s.Domains) > 0 {
		ui.Print("  Domain:  %s", s.Domains[0])
		for _, alias := range s.Domains[1:] {
			ui.Print("  Alias:   %s", alias)
		}
	}
	ui.Print("  SSL:     %s", ui.TypeColor(s.IsLocal))

	// Site type info
	meta, _ := site.ReadSiteMetadata(s.Name)
	switch s.Type {
	case site.SiteTypeStatic:
		ui.Print("  Type:    %s", "static (nginx)")
	case site.SiteTypePHP:
		typeLabel := "php (nginx + php-fpm)"
		if meta != nil && meta.PHPVersion != "" && meta.PHPVersion != "latest" {
			typeLabel = fmt.Sprintf("php %s (nginx + php-fpm)", meta.PHPVersion)
		}
		ui.Print("  Type:    %s", typeLabel)
		if meta != nil && meta.PHPFramework != "" && meta.PHPFramework != "generic" {
			ui.Print("  Framework: %s", meta.PHPFramework)
		}
		if meta != nil && len(meta.PHPExtensions) > 0 {
			ui.Print("  Extensions: %d loaded", len(meta.PHPExtensions))
		}
	case site.SiteTypeNode:
		runtimeLabel := "node.js"
		if meta != nil && meta.NodeRuntime != "" {
			runtimeLabel = meta.NodeRuntime
			if meta.NodePackageManager != "" && meta.NodePackageManager != meta.NodeRuntime && meta.NodePackageManager != constants.NodePMDeno {
				runtimeLabel += " / " + meta.NodePackageManager
			}
			if meta.NodeVersion != "" && meta.NodeVersion != constants.NodeVersionLTS {
				runtimeLabel += " " + meta.NodeVersion
			}
		}
		ui.Print("  Type:    %s", runtimeLabel)
		if meta != nil && meta.NodeFramework != "" && meta.NodeFramework != "generic" {
			ui.Print("  Framework: %s", meta.NodeFramework)
		}
		if s.Port != 0 {
			ui.Print("  Port:    %d", s.Port)
		}
	case site.SiteTypeRuby:
		runtimeLabel := "ruby"
		if meta != nil && meta.RubyVersion != "" && meta.RubyVersion != constants.RubyVersionLatest {
			runtimeLabel = "ruby " + meta.RubyVersion
		}
		ui.Print("  Type:    %s", runtimeLabel)
		if meta != nil && meta.RubyFramework != "" && meta.RubyFramework != "generic" {
			ui.Print("  Framework: %s", meta.RubyFramework)
		}
		if s.Port != 0 {
			ui.Print("  Port:    %d", s.Port)
		}
	case site.SiteTypePython:
		runtimeLabel := "python"
		if meta != nil && meta.PythonVersion != "" && meta.PythonVersion != constants.PythonVersionLatest {
			runtimeLabel = "python " + meta.PythonVersion
		}
		ui.Print("  Type:    %s", runtimeLabel)
		if meta != nil && meta.PythonFramework != "" && meta.PythonFramework != "generic" {
			ui.Print("  Framework: %s", meta.PythonFramework)
		}
		if s.Port != 0 {
			ui.Print("  Port:    %d", s.Port)
		}
	case site.SiteTypeDockerfile:
		ui.Print("  Type:    %s", "dockerfile (custom build)")
		if s.Port != 0 {
			ui.Print("  Port:    %d", s.Port)
		}
	default:
		ui.Print("  Type:    %s", "compose")
		if s.ServiceName != "" {
			ui.Print("  Service: %s", s.ServiceName)
		}
		if s.Port != 0 {
			ui.Print("  Port:    %d", s.Port)
		}
	}

	cfg, _ := config.Load()
	if cfg != nil {
		ui.Print("  Config:  %s/sites/%s/", cfg.Root, s.Name)
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
	if s.IsLocal && s.Domain() != "" {
		showCertInfo(s.Domain())
	}

	// Show URL if running
	if s.Status == constants.StatusRunning && s.Domain() != "" {
		ui.Blank()
		ui.Info("URL: https://%s", s.Domain())
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
// Returns an error if any site operation fails.
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
	var failCount atomic.Int32
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
					failCount.Add(1)
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

	if n := failCount.Load(); n > 0 {
		return fmt.Errorf("%d site(s) failed to %s", n, opName)
	}
	return nil
}

// =============================================================================
// logs command
// =============================================================================

var logsFlags struct {
	follow bool
	all    bool
	tail   string
	since  string
}

var logsCmd = &cobra.Command{
	Use:   "logs [SITE]",
	Short: "Show site logs",
	Args: func(cmd *cobra.Command, args []string) error {
		if logsFlags.all {
			return cobra.NoArgs(cmd, args)
		}
		if len(args) == 0 {
			_ = cmd.Help()
			return ui.UsageError("srv logs SITE", "a site name is required (or pass --all)")
		}
		if len(args) > 1 {
			return ui.UsageError("srv logs SITE", "too many arguments — expected a single site name, got %d", len(args))
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
	logsCmd.Flags().BoolVarP(&logsFlags.all, "all", "a", false, "Multiplex logs from every running site (colour-prefixed)")
	logsCmd.Flags().StringVar(&logsFlags.tail, "tail", "", "Number of lines to show from the end")
	logsCmd.Flags().StringVar(&logsFlags.since, "since", "", "Show logs since timestamp (e.g., 10m, 1h)")
	logsCmd.GroupID = GroupSites
	RootCmd.AddCommand(logsCmd)
}

func runLogs(cmd *cobra.Command, args []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	if logsFlags.all {
		return runLogsAll()
	}

	s, err := site.GetByName(args[0])
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

// runLogsAll multiplexes `docker compose logs` for every non-broken site,
// prefixing each output line with the site name. Stops when stdin closes
// (Ctrl-C) or when --follow is off and every per-site tail completes.
func runLogsAll() error {
	sites, err := site.List()
	if err != nil {
		return err
	}
	var running []site.Site
	for _, s := range sites {
		if !s.IsBroken {
			running = append(running, s)
		}
	}
	if len(running) == 0 {
		ui.Dim("No sites registered")
		return nil
	}

	var wg sync.WaitGroup
	for _, s := range running {
		s := s
		wg.Add(1)
		go func() {
			defer wg.Done()
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
			// Prefix every line with the site name. ComposePrefixed shells out
			// to `docker compose logs` and streams output through a writer that
			// stamps each line.
			if err := docker.ComposePrefixed(s.ComposeDir, s.Name, composeArgs...); err != nil {
				ui.Warn("[%s] log stream ended: %v", s.Name, err)
			}
		}()
	}
	wg.Wait()
	return nil
}

// =============================================================================
// runtime command
// =============================================================================

var runtimeFlags struct {
	phpVersion    string
	phpExtensions string
	nodeVersion   string
}

var runtimeCmd = &cobra.Command{
	Use:   "runtime SITE",
	Short: "Change runtime version or PHP extensions for a site",
	Long: `Update the runtime version or PHP extensions for a PHP or Node.js site.

For PHP sites the Dockerfile is rebuilt with the new version / extension list.
php.ini and nginx.conf are left untouched so your customisations are preserved.

For Node.js sites the docker-compose.yml is regenerated with the new version.

After updating, the site containers are rebuilt and restarted automatically.

Examples:
  srv site runtime mysite --php-version 8.3
  srv site runtime mysite --php-extensions "+redis,-xdebug"
  srv site runtime mysite --node-version 22`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			_ = cmd.Help()
			return ui.UsageError("srv site runtime SITE", "a site name is required")
		}
		if len(args) > 1 {
			return ui.UsageError("srv site runtime SITE", "too many arguments — expected a single site name, got %d", len(args))
		}
		return nil
	},
	RunE: runRuntime,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	runtimeCmd.Flags().StringVar(&runtimeFlags.phpVersion, "php-version", "", "PHP version (e.g. 8.3, 8.2, latest)")
	runtimeCmd.Flags().StringVar(&runtimeFlags.phpExtensions, "php-extensions", "", "PHP extensions: full list, or +ext/-ext to add/remove from defaults")
	runtimeCmd.Flags().StringVar(&runtimeFlags.nodeVersion, "node-version", "", "Node.js version (e.g. 22, 20, lts)")
	_ = runtimeCmd.RegisterFlagCompletionFunc("php-extensions", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return site.KnownPHPExtensions(), cobra.ShellCompDirectiveNoFileComp
	})
	runtimeCmd.GroupID = GroupSites
	RootCmd.AddCommand(runtimeCmd)
}

func runRuntime(cmd *cobra.Command, args []string) error {
	siteName := args[0]

	meta, err := site.ReadSiteMetadata(siteName)
	if err != nil || meta == nil {
		return fmt.Errorf("site '%s' not found", siteName)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	switch meta.Type {
	case site.SiteTypePHP:
		return runtimeUpdatePHP(cfg, siteName, meta)
	case site.SiteTypeNode:
		return runtimeUpdateNode(cfg, siteName, meta)
	default:
		return fmt.Errorf("site '%s' is a %s site — runtime management is only available for PHP and Node.js sites", siteName, meta.Type)
	}
}

func runtimeUpdatePHP(cfg *config.Config, siteName string, meta *site.SiteMetadata) error {
	if runtimeFlags.phpVersion == "" && runtimeFlags.phpExtensions == "" {
		return fmt.Errorf("specify at least --php-version or --php-extensions")
	}

	// Show before/after diff.
	if runtimeFlags.phpVersion != "" && runtimeFlags.phpVersion != meta.PHPVersion {
		ui.Dim("  php version:  %s → %s", meta.PHPVersion, runtimeFlags.phpVersion)
		meta.PHPVersion = runtimeFlags.phpVersion
	}
	if runtimeFlags.phpExtensions != "" {
		before := len(meta.PHPExtensions)
		meta.PHPExtensions = site.ParseExtensionOverrides(runtimeFlags.phpExtensions, meta.PHPExtensions)
		after := len(meta.PHPExtensions)
		if before != after {
			ui.Dim("  extensions:   %d → %d", before, after)
		}
	}

	phpInfo := &site.PHPSiteInfo{
		PHPVersion:   meta.PHPVersion,
		Extensions:   meta.PHPExtensions,
		Framework:    meta.PHPFramework,
		DocumentRoot: meta.DocumentRoot,
	}

	ui.Info("Updating PHP runtime for '%s'...", siteName)
	if err := site.WritePHPDockerConfig(siteName, *meta, phpInfo); err != nil {
		return fmt.Errorf("failed to write PHP config: %w", err)
	}

	// Persist updated metadata.
	if err := site.WriteSiteMetadata(siteName, *meta); err != nil {
		return fmt.Errorf("failed to update metadata: %w", err)
	}

	composeDir := site.SiteConfigDir(cfg, siteName)
	ui.Info("Rebuilding and restarting containers...")
	if err := docker.ComposeUpBuildWithProfile(composeDir, meta.Profile); err != nil {
		return fmt.Errorf("failed to rebuild containers: %w", err)
	}

	ui.Success("PHP runtime updated for '%s' (version: %s)", siteName, meta.PHPVersion)
	return nil
}

func runtimeUpdateNode(cfg *config.Config, siteName string, meta *site.SiteMetadata) error {
	if runtimeFlags.nodeVersion == "" {
		return fmt.Errorf("specify --node-version")
	}

	if meta.NodeRuntime != "" && meta.NodeRuntime != constants.NodeRuntimeNode {
		return fmt.Errorf("--node-version only applies to Node.js sites (this site uses %s)", meta.NodeRuntime)
	}

	if runtimeFlags.nodeVersion != meta.NodeVersion {
		ui.Dim("  node version:  %s → %s", meta.NodeVersion, runtimeFlags.nodeVersion)
	}
	meta.NodeVersion = runtimeFlags.nodeVersion

	nodeInfo := &site.NodeSiteInfo{
		Runtime:        meta.NodeRuntime,
		PackageManager: meta.NodePackageManager,
		NodeVersion:    meta.NodeVersion,
		Framework:      meta.NodeFramework,
		StartCmd:       meta.NodeStartCmd,
		Port:           meta.Port,
	}

	ui.Info("Updating Node.js runtime for '%s'...", siteName)
	if err := site.WriteNodeSiteConfig(siteName, *meta, nodeInfo, true); err != nil {
		return fmt.Errorf("failed to write Node config: %w", err)
	}

	// Persist updated metadata.
	if err := site.WriteSiteMetadata(siteName, *meta); err != nil {
		return fmt.Errorf("failed to update metadata: %w", err)
	}

	composeDir := site.SiteConfigDir(cfg, siteName)
	ui.Info("Restarting containers...")
	if err := docker.ComposeUpWithProfile(composeDir, meta.Profile); err != nil {
		return fmt.Errorf("failed to restart containers: %w", err)
	}

	ui.Success("Node.js runtime updated for '%s' (version: %s)", siteName, meta.NodeVersion)
	return nil
}

// =============================================================================
// regenerate command
// =============================================================================

var regenerateCmd = &cobra.Command{
	Use:   "regenerate SITE",
	Short: "Regenerate config files for a site, overwriting any customisations",
	Long: `Regenerate all config files (nginx.conf, php.ini, docker-compose.yml, Dockerfile)
for a site from scratch, overwriting any manual edits.

Use this if you want to reset your config to srv defaults, or after changing
the domain / SSL type.

The site will be restarted automatically after regeneration.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			_ = cmd.Help()
			return ui.UsageError("srv site regenerate SITE", "a site name is required")
		}
		if len(args) > 1 {
			return ui.UsageError("srv site regenerate SITE", "too many arguments — expected a single site name, got %d", len(args))
		}
		return nil
	},
	RunE: runRegenerate,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	regenerateCmd.GroupID = GroupSites
	RootCmd.AddCommand(regenerateCmd)
}

func runRegenerate(cmd *cobra.Command, args []string) error {
	siteName := args[0]

	meta, err := site.ReadSiteMetadata(siteName)
	if err != nil || meta == nil {
		return fmt.Errorf("site '%s' not found", siteName)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Confirm with user before overwriting.
	var confirmed bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Regenerate config for '%s'?", siteName)).
				Description("This will overwrite nginx.conf, php.ini, docker-compose.yml, and Dockerfile.\nYour manual edits will be lost.").
				Value(&confirmed),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}
	if !confirmed {
		ui.Dim("Cancelled.")
		return nil
	}

	ui.Info("Regenerating config for '%s'...", siteName)

	switch meta.Type {
	case site.SiteTypePHP:
		phpInfo := &site.PHPSiteInfo{
			PHPVersion:   meta.PHPVersion,
			Extensions:   meta.PHPExtensions,
			Framework:    meta.PHPFramework,
			DocumentRoot: meta.DocumentRoot,
		}
		if err := site.WritePHPSiteConfig(siteName, *meta, phpInfo, true); err != nil {
			return fmt.Errorf("failed to regenerate PHP config: %w", err)
		}
	case site.SiteTypeNode:
		nodeInfo := &site.NodeSiteInfo{
			Runtime:        meta.NodeRuntime,
			PackageManager: meta.NodePackageManager,
			NodeVersion:    meta.NodeVersion,
			Framework:      meta.NodeFramework,
			StartCmd:       meta.NodeStartCmd,
			Port:           meta.Port,
		}
		if err := site.WriteNodeSiteConfig(siteName, *meta, nodeInfo, true); err != nil {
			return fmt.Errorf("failed to regenerate Node config: %w", err)
		}
	case site.SiteTypeRuby:
		rubyInfo := &site.RubySiteInfo{
			RubyVersion: meta.RubyVersion,
			Framework:   meta.RubyFramework,
			StartCmd:    meta.RubyStartCmd,
			Port:        meta.Port,
		}
		if err := site.WriteRubySiteConfig(siteName, *meta, rubyInfo, true); err != nil {
			return fmt.Errorf("failed to regenerate Ruby config: %w", err)
		}
	case site.SiteTypePython:
		pythonInfo := &site.PythonSiteInfo{
			PythonVersion: meta.PythonVersion,
			Framework:     meta.PythonFramework,
			StartCmd:      meta.PythonStartCmd,
			Port:          meta.Port,
		}
		if err := site.WritePythonSiteConfig(siteName, *meta, pythonInfo, true); err != nil {
			return fmt.Errorf("failed to regenerate Python config: %w", err)
		}
	case site.SiteTypeDockerfile:
		dockerfileInfo := &site.DockerfileSiteInfo{Port: meta.DockerfilePort}
		if err := site.WriteDockerfileSiteConfig(siteName, *meta, dockerfileInfo, true); err != nil {
			return fmt.Errorf("failed to regenerate Dockerfile config: %w", err)
		}
	case site.SiteTypeStatic:
		if err := site.WriteStaticSiteConfig(siteName, *meta, true); err != nil {
			return fmt.Errorf("failed to regenerate static config: %w", err)
		}
	default:
		return fmt.Errorf("regenerate is only available for PHP, Node.js, Ruby, Python, Dockerfile, and static sites")
	}

	composeDir := site.SiteConfigDir(cfg, siteName)
	ui.Info("Restarting containers...")
	if err := docker.ComposeUpBuildWithProfile(composeDir, meta.Profile); err != nil {
		return fmt.Errorf("failed to restart containers: %w", err)
	}

	ui.Success("Config regenerated for '%s'", siteName)
	return nil
}

// =============================================================================
// edit command
// =============================================================================

var editCmd = &cobra.Command{
	Use:   "edit SITE",
	Short: "Open the config directory for a site in your editor",
	Long: `Open the srv config directory for a site in $EDITOR (or $VISUAL).

The config directory contains nginx.conf, php.ini, docker-compose.yml,
and the Dockerfile — all of which you can freely edit.

If no editor is configured, the path is printed so you can open it manually.

After editing, run "srv site restart SITE" for changes to take effect.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			_ = cmd.Help()
			return ui.UsageError("srv site edit SITE", "a site name is required")
		}
		if len(args) > 1 {
			return ui.UsageError("srv site edit SITE", "too many arguments — expected a single site name, got %d", len(args))
		}
		return nil
	},
	RunE: runEdit,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	editCmd.GroupID = GroupSites
	RootCmd.AddCommand(editCmd)
}

func runEdit(cmd *cobra.Command, args []string) error {
	siteName := args[0]

	if !site.Exists(siteName) {
		return fmt.Errorf("site '%s' not found", siteName)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	siteDir := site.SiteConfigDir(cfg, siteName)

	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}

	if editor == "" {
		ui.Print("Config directory for '%s':", siteName)
		ui.Print("  %s", siteDir)
		ui.Blank()
		ui.Dim("Set $EDITOR or $VISUAL to open it automatically.")
		ui.Dim("After editing, run: srv site restart %s", siteName)
		return nil
	}

	ui.Dim("Opening %s in %s...", siteDir, editor)
	c := exec.Command(editor, siteDir) //nolint:gosec // editor from trusted env var
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("editor exited with error: %w", err)
	}

	ui.Dim("Run 'srv site restart %s' for changes to take effect.", siteName)
	return nil
}

// =============================================================================
// shell command
// =============================================================================

var shellFlags struct {
	service string
}

var shellCmd = &cobra.Command{
	Use:   "shell SITE",
	Short: "Open an interactive shell in a site's container",
	Long: `Open an interactive shell (sh) in the primary container for a site.

For PHP sites the default is the php-fpm container (srv-SITE-php).
Use --service web to get a shell in the nginx container instead.

For Node, Ruby, Python, and Dockerfile sites the single app container is used.

For compose sites the first service container is used; use --service to pick one.

Examples:
  srv site shell mysite
  srv site shell mysite --service web   # nginx container for PHP sites`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			_ = cmd.Help()
			return ui.UsageError("srv site shell SITE", "a site name is required")
		}
		if len(args) > 1 {
			return ui.UsageError("srv site shell SITE", "too many arguments — expected a single site name, got %d", len(args))
		}
		return nil
	},
	RunE: runShell,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	shellCmd.Flags().StringVar(&shellFlags.service, "service", "", "Container name or service to shell into")
	shellCmd.GroupID = GroupSites
	RootCmd.AddCommand(shellCmd)
}

func runShell(cmd *cobra.Command, args []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	siteName := args[0]
	s, err := site.GetByName(siteName)
	if err != nil {
		return err
	}

	if s.IsBroken {
		return fmt.Errorf("site '%s' is broken (target directory missing)", s.Name)
	}

	// Determine the container to shell into.
	containerName := shellFlags.service
	if containerName == "" {
		containerName = siteShellContainer(*s)
	}

	if containerName == "" {
		return fmt.Errorf("cannot determine container for site '%s' — use --service to specify one", siteName)
	}

	if !docker.ContainerExists(containerName) {
		return fmt.Errorf("container '%s' is not running — start the site first with: srv start %s", containerName, siteName)
	}

	ui.Dim("Connecting to container: %s", containerName)
	execArgs := []string{"exec", "-it"}
	// For PHP sites the shell lands in the shared pool container; set the
	// working directory to this site's mount so paths feel per-site.
	if s.Type == site.SiteTypePHP {
		execArgs = append(execArgs, "-w", "/var/www/"+siteName)
	}
	execArgs = append(execArgs, containerName, "sh")
	c := exec.Command("docker", execArgs...) //nolint:gosec
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		// Exit code != 0 from the shell is normal (user typed exit N), don't wrap it as an error.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
			return nil
		}
		return fmt.Errorf("docker exec failed: %w", err)
	}
	return nil
}

// siteShellContainer returns the container name to shell into for a given site.
// For PHP sites this is the shared pool container; the caller is expected to
// set the working directory to /var/www/<sitename> when execing.
func siteShellContainer(s site.Site) string {
	switch s.Type {
	case site.SiteTypePHP:
		return phpFPMContainerForSite(s.Name)
	case site.SiteTypeNode:
		return "srv-" + s.Name + "-node"
	case site.SiteTypeRuby, site.SiteTypePython, site.SiteTypeDockerfile:
		return "srv-" + s.Name + "-app"
	default:
		// Compose sites: use the stored service name (container name).
		return s.ServiceName
	}
}

// phpFPMContainerForSite resolves a PHP site to its shared FPM container name
// by reading the site's metadata and computing the pool fingerprint. Falls
// back to the legacy per-site container name if metadata is missing.
func phpFPMContainerForSite(siteName string) string {
	meta, err := site.ReadSiteMetadata(siteName)
	if err != nil || meta == nil {
		return "srv-" + siteName + "-php"
	}
	exts := make([]string, 0, len(meta.PHPExtensions))
	for _, e := range meta.PHPExtensions {
		if !site.IsBuiltinPHPExtension(e) {
			exts = append(exts, e)
		}
	}
	return "srv-fpm-" + pool.Fingerprint(meta.PHPVersion, exts)
}

// =============================================================================
// open command
// =============================================================================

var openCmd = &cobra.Command{
	Use:   "open SITE",
	Short: "Open a site in the default browser",
	Long:  `Open the site's HTTPS URL in the system default browser using xdg-open.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			_ = cmd.Help()
			return ui.UsageError("srv site open SITE", "a site name is required")
		}
		if len(args) > 1 {
			return ui.UsageError("srv site open SITE", "too many arguments — expected a single site name, got %d", len(args))
		}
		return nil
	},
	RunE: runOpen,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	openCmd.GroupID = GroupSites
	RootCmd.AddCommand(openCmd)
}

func runOpen(cmd *cobra.Command, args []string) error {
	siteName := args[0]
	s, err := site.GetByName(siteName)
	if err != nil {
		return err
	}

	primary := s.Domain()
	if primary == "" {
		return fmt.Errorf("site '%s' has no domain configured", siteName)
	}

	url := "https://" + primary
	ui.Dim("Opening %s...", url)
	c := exec.Command("xdg-open", url) //nolint:gosec
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("xdg-open failed: %w", err)
	}
	return nil
}

// =============================================================================
// Helper functions
// =============================================================================

// detectionSummary returns a human-readable summary of what was detected for a site.
func detectionSummary(setup *siteSetup) string {
	switch {
	case setup.composePath != "":
		return "docker-compose project"
	case setup.isPHP && setup.phpInfo != nil:
		fw := setup.phpInfo.Framework
		ver := setup.phpInfo.PHPVersion
		ext := len(setup.phpInfo.Extensions)
		if fw != "generic" {
			return fmt.Sprintf("%s (PHP %s, %d extensions)", fw, ver, ext)
		}
		return fmt.Sprintf("php (PHP %s, %d extensions)", ver, ext)
	case setup.isNode && setup.nodeInfo != nil:
		info := setup.nodeInfo
		runtime := info.Runtime
		if info.PackageManager != info.Runtime && info.PackageManager != constants.NodePMDeno {
			runtime += " / " + info.PackageManager
		}
		fw := info.Framework
		ver := info.NodeVersion
		if fw != "generic" {
			return fmt.Sprintf("%s (%s %s)", fw, runtime, ver)
		}
		return fmt.Sprintf("%s %s", runtime, ver)
	case setup.isRuby && setup.rubyInfo != nil:
		info := setup.rubyInfo
		if info.Framework != constants.RubyFrameworkGeneric {
			return fmt.Sprintf("%s (ruby %s)", info.Framework, info.RubyVersion)
		}
		return fmt.Sprintf("ruby %s", info.RubyVersion)
	case setup.isPython && setup.pythonInfo != nil:
		info := setup.pythonInfo
		if info.Framework != constants.PythonFrameworkGeneric {
			return fmt.Sprintf("%s (python %s)", info.Framework, info.PythonVersion)
		}
		return fmt.Sprintf("python %s", info.PythonVersion)
	case setup.isDockerfile && setup.dockerfileInfo != nil:
		return fmt.Sprintf("Dockerfile (port %d)", setup.dockerfileInfo.Port)
	case setup.isStatic:
		return "static site"
	default:
		return "unknown"
	}
}
