// Package cmd — site_add.go is the entry point for `srv add`: cobra wiring,
// the `siteSetup` struct that threads through the rest of the flow
// (site_add_detect/prompt/files/finalize), and the top-level orchestration.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
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

// allDomains returns the canonical domain followed by any aliases.
func (s *siteSetup) allDomains() []string {
	out := make([]string, 0, 1+len(s.aliases))
	if s.domain != "" {
		out = append(out, s.domain)
	}
	out = append(out, s.aliases...)
	return out
}
