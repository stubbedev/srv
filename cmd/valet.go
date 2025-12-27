package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

// =============================================================================
// link command (Valet-style alias for add)
// =============================================================================

var linkFlags struct {
	port string
}

var linkCmd = &cobra.Command{
	Use:   "link [NAME]",
	Short: "Link current directory as a site",
	Long: `Link the current directory as a site with srv.

This is a Valet-style convenience command. If no name is provided,
the directory name is used as the site name.

If the directory contains a docker-compose.yml, it will be used.
Otherwise, srv will automatically serve the directory as static files
using nginx.

The site will be accessible at NAME.test with local SSL.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runLink,
}

func init() {
	linkCmd.Flags().StringVarP(&linkFlags.port, "port", "p", "80", "Container port (for docker-compose sites)")
	RootCmd.AddCommand(linkCmd)
}

func runLink(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// Determine site name
	siteName := site.SanitizeName(cwd)
	if len(args) > 0 {
		siteName = args[0]
	}

	domain := siteName + ".test"
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Check if site already exists
	if site.Exists(siteName) {
		return fmt.Errorf("site '%s' already exists. Use 'srv remove %s' first", siteName, siteName)
	}

	// Check for compose file
	composePath, err := site.FindComposeFile(cwd)
	if err != nil {
		// No docker-compose file found - serve as static site
		return linkStaticSite(cwd, siteName, domain, cfg)
	}

	// Docker-compose file exists - link as compose-based site
	return linkComposeSite(cwd, siteName, domain, composePath, cfg)
}

// linkStaticSite links a directory as a static file site using nginx
func linkStaticSite(cwd, siteName, domain string, cfg *config.Config) error {
	ui.Info("Linking static site: %s", siteName)

	// Write env.site
	if err := site.WriteEnvFile(cwd, domain, true, cfg.NetworkName); err != nil {
		return fmt.Errorf("failed to write env.site: %w", err)
	}

	// Write static site docker-compose.yml
	if err := site.WriteStaticSiteCompose(cwd, siteName, domain, cfg.NetworkName); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	// Generate SSL certificate
	generateLinkCert(domain)

	// Register site
	if err := site.Register(siteName, cwd); err != nil {
		return fmt.Errorf("failed to register site: %w", err)
	}

	ui.Success("Static site '%s' linked!", siteName)
	ui.Dim("https://%s", domain)
	ui.Blank()
	ui.Dim("Serving files from: %s", cwd)
	ui.Dim("Run 'srv start %s' to start the site", siteName)
	return nil
}

// linkComposeSite links a directory with an existing docker-compose file
func linkComposeSite(cwd, siteName, domain, composePath string, cfg *config.Config) error {
	// Get service name
	services, err := site.GetServiceNames(composePath)
	if err != nil {
		return fmt.Errorf("failed to parse compose file: %w", err)
	}
	if len(services) == 0 {
		return fmt.Errorf("no services found in compose file")
	}

	serviceName := services[0]
	if len(services) > 1 {
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

	ui.Info("Linking site: %s", siteName)

	if err := site.WriteEnvFile(cwd, domain, true, cfg.NetworkName); err != nil {
		return fmt.Errorf("failed to write env.site: %w", err)
	}

	if err := site.WriteSiteCompose(cwd, serviceName, siteName, domain, linkFlags.port, true, cfg.NetworkName); err != nil {
		return fmt.Errorf("failed to write docker-compose.site.yml: %w", err)
	}

	// Generate SSL certificate
	generateLinkCert(domain)

	// Add include to docker-compose.yml
	if added, err := site.EnsureSiteComposeInclude(composePath); err != nil {
		ui.Warn("Warning: Could not update %s: %v", filepath.Base(composePath), err)
		ui.Blank()
		ui.Warn("Add this to your docker-compose.yml manually:")
		ui.Code("  include:")
		ui.Code("    - docker-compose.site.yml")
	} else if added {
		ui.Dim("Added include to %s", filepath.Base(composePath))
	}

	// Register site
	if err := site.Register(siteName, cwd); err != nil {
		return fmt.Errorf("failed to register site: %w", err)
	}

	ui.Success("Site '%s' linked!", siteName)
	ui.Dim("https://%s", domain)
	ui.Blank()
	ui.Dim("Run 'srv start %s' to start the site", siteName)
	return nil
}

// generateLinkCert generates an SSL certificate for a linked site
func generateLinkCert(domain string) {
	if err := traefik.CheckMkcert(); err == nil && traefik.IsCAInstalled() {
		if err := traefik.EnsureLocalCert(domain); err != nil {
			ui.Warn("Warning: Failed to generate certificate: %v", err)
		} else {
			if err := traefik.UpdateDynamicConfig(); err != nil {
				ui.Warn("Warning: Failed to update Traefik config: %v", err)
			}
		}
	}
}

// =============================================================================
// unlink command
// =============================================================================

var unlinkCmd = &cobra.Command{
	Use:   "unlink [NAME]",
	Short: "Unlink a site (alias for remove)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runUnlink,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	RootCmd.AddCommand(unlinkCmd)
}

func runUnlink(cmd *cobra.Command, args []string) error {
	var siteName string

	if len(args) > 0 {
		siteName = args[0]
	} else {
		// Try to find site from current directory
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("no site specified and could not get current directory")
		}

		sites, err := site.List()
		if err != nil {
			return err
		}

		for _, s := range sites {
			if s.Dir == cwd {
				siteName = s.Name
				break
			}
		}

		if siteName == "" {
			return fmt.Errorf("no site specified and current directory is not a registered site")
		}
	}

	// Delegate to runRemove
	return runRemove(cmd, []string{siteName})
}

// =============================================================================
// links command
// =============================================================================

var linksCmd = &cobra.Command{
	Use:   "links",
	Short: "List all linked sites (alias for list)",
	RunE:  runList,
}

func init() {
	RootCmd.AddCommand(linksCmd)
}

// =============================================================================
// open command
// =============================================================================

var openCmd = &cobra.Command{
	Use:   "open [SITE]",
	Short: "Open a site in the browser",
	Long: `Open a site in your default web browser.

If no site is specified and you're in a site directory, opens that site.
Otherwise, opens the Traefik dashboard.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runOpen,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	RootCmd.AddCommand(openCmd)
}

func runOpen(cmd *cobra.Command, args []string) error {
	var url string

	if len(args) > 0 {
		// Open specific site
		s, err := site.Get(args[0])
		if err != nil {
			return err
		}
		if s.Domain == "" {
			return fmt.Errorf("site '%s' has no domain configured", s.Name)
		}
		url = "https://" + s.Domain
	} else {
		// Try to detect current directory site
		cwd, err := os.Getwd()
		if err == nil {
			sites, _ := site.List()
			for _, s := range sites {
				if s.Dir == cwd {
					url = "https://" + s.Domain
					break
				}
			}
		}

		// Fall back to dashboard
		if url == "" {
			url = traefik.DashboardURL()
		}
	}

	ui.Info("Opening %s", url)
	return openBrowser(url)
}

func openBrowser(url string) error {
	var cmd string
	var args []string

	switch {
	case CommandExists("xdg-open"):
		cmd = "xdg-open"
		args = []string{url}
	case CommandExists("open"):
		cmd = "open"
		args = []string{url}
	case CommandExists("wslview"):
		cmd = "wslview"
		args = []string{url}
	default:
		return fmt.Errorf("no browser opener found. Please open manually: %s", url)
	}

	return RunCommand(cmd, args...)
}

// =============================================================================
// secure command
// =============================================================================

var secureCmd = &cobra.Command{
	Use:   "secure [SITE]",
	Short: "Secure a site with local SSL",
	Long: `Enable local SSL (HTTPS) for a site using mkcert.

If no site is specified and you're in a site directory, secures that site.
This generates a trusted SSL certificate for local development.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSecure,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	RootCmd.AddCommand(secureCmd)
}

func runSecure(cmd *cobra.Command, args []string) error {
	s, err := GetSiteFromArgsRequired(args)
	if err != nil {
		return err
	}

	if s.IsLocal {
		ui.Info("Site '%s' is already secured with local SSL", s.Name)
		return nil
	}

	// Check mkcert
	if err := traefik.CheckMkcert(); err != nil {
		return err
	}

	if !traefik.IsCAInstalled() {
		return fmt.Errorf("mkcert CA not installed. Run 'srv trust' first")
	}

	// Generate certificate
	ui.Info("Generating SSL certificate for %s...", s.Domain)
	if err := traefik.GenerateLocalCert(s.Domain); err != nil {
		return fmt.Errorf("failed to generate certificate: %w", err)
	}

	// Update env.site
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := site.WriteEnvFile(s.Dir, s.Domain, true, cfg.NetworkName); err != nil {
		return fmt.Errorf("failed to update env.site: %w", err)
	}

	// Find compose file and update docker-compose.site.yml
	composePath, err := site.FindComposeFile(s.Dir)
	if err == nil {
		services, _ := site.GetServiceNames(composePath)
		if len(services) > 0 {
			// Re-generate site compose with local SSL enabled
			if err := site.WriteSiteCompose(s.Dir, services[0], s.Name, s.Domain, "80", true, cfg.NetworkName); err != nil {
				ui.Warn("Warning: Could not update docker-compose.site.yml: %v", err)
			}
		}
	}

	// Update Traefik dynamic config
	if err := traefik.UpdateDynamicConfig(); err != nil {
		ui.Warn("Warning: Failed to update Traefik config: %v", err)
	}

	// Restart site if running
	if s.Status == "running" {
		ui.Info("Restarting site...")
		if err := docker.ComposeRestart(s.Dir); err != nil {
			ui.Warn("Warning: Failed to restart site: %v", err)
		}
	}

	ui.Success("Site '%s' is now secured with local SSL", s.Name)
	ui.Dim("https://%s", s.Domain)
	return nil
}

// =============================================================================
// unsecure command
// =============================================================================

var unsecureCmd = &cobra.Command{
	Use:   "unsecure [SITE]",
	Short: "Remove local SSL from a site",
	Long: `Disable local SSL for a site, reverting to Let's Encrypt (production) SSL.

If no site is specified and you're in a site directory, unsecures that site.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runUnsecure,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	RootCmd.AddCommand(unsecureCmd)
}

func runUnsecure(cmd *cobra.Command, args []string) error {
	s, err := GetSiteFromArgsRequired(args)
	if err != nil {
		return err
	}

	if !s.IsLocal {
		ui.Info("Site '%s' is not using local SSL", s.Name)
		return nil
	}

	// Remove certificate
	if err := traefik.RemoveLocalCerts(s.Domain); err != nil {
		ui.Warn("Warning: Failed to remove certificate: %v", err)
	}

	// Update env.site
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := site.WriteEnvFile(s.Dir, s.Domain, false, cfg.NetworkName); err != nil {
		return fmt.Errorf("failed to update env.site: %w", err)
	}

	// Find compose file and update docker-compose.site.yml
	composePath, err := site.FindComposeFile(s.Dir)
	if err == nil {
		services, _ := site.GetServiceNames(composePath)
		if len(services) > 0 {
			// Re-generate site compose with local SSL disabled
			if err := site.WriteSiteCompose(s.Dir, services[0], s.Name, s.Domain, "80", false, cfg.NetworkName); err != nil {
				ui.Warn("Warning: Could not update docker-compose.site.yml: %v", err)
			}
		}
	}

	// Update Traefik dynamic config
	if err := traefik.UpdateDynamicConfig(); err != nil {
		ui.Warn("Warning: Failed to update Traefik config: %v", err)
	}

	// Restart site if running
	if s.Status == "running" {
		ui.Info("Restarting site...")
		if err := docker.ComposeRestart(s.Dir); err != nil {
			ui.Warn("Warning: Failed to restart site: %v", err)
		}
	}

	ui.Success("Site '%s' is now using Let's Encrypt SSL", s.Name)
	ui.Warn("Note: Let's Encrypt requires a public domain and DNS pointing to this server")
	return nil
}

// =============================================================================
// paths command
// =============================================================================

var pathsCmd = &cobra.Command{
	Use:   "paths",
	Short: "Show srv configuration paths",
	Long:  `Display all directories and files used by srv.`,
	RunE:  runPaths,
}

func init() {
	RootCmd.AddCommand(pathsCmd)
}

func runPaths(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ui.Bold("Configuration paths:")
	ui.Blank()
	ui.Print("  Config root:     %s", cfg.Root)
	ui.Print("  Traefik config:  %s", cfg.TraefikDir)
	ui.Print("  Sites directory: %s", cfg.SitesDir)
	ui.Print("  Local certs:     %s", cfg.LocalCertsDir())
	ui.Print("  Traefik conf:    %s", cfg.TraefikConfDir())
	ui.Blank()
	ui.Print("  Docker network:  %s", cfg.NetworkName)

	// Show parked paths if any
	parkedPaths, _ := LoadParkedPaths(cfg)
	if len(parkedPaths) > 0 {
		ui.Blank()
		ui.Bold("Parked directories:")
		for _, p := range parkedPaths {
			ui.Print("  %s", p)
		}
	}

	return nil
}

// =============================================================================
// share command
// =============================================================================

var shareFlags struct {
	tool string
}

var shareCmd = &cobra.Command{
	Use:   "share [SITE]",
	Short: "Share a site publicly via tunnel",
	Long: `Share a local site publicly using a tunnel service.

Supported tools:
  - cloudflared (Cloudflare Tunnel) - recommended
  - ngrok

If no site is specified and you're in a site directory, shares that site.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runShare,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	shareCmd.Flags().StringVar(&shareFlags.tool, "tool", "", "Tunnel tool to use (cloudflared, ngrok)")
	RootCmd.AddCommand(shareCmd)
}

func runShare(cmd *cobra.Command, args []string) error {
	s, err := GetSiteFromArgsRequired(args)
	if err != nil {
		return err
	}

	if s.Domain == "" {
		return fmt.Errorf("site '%s' has no domain configured", s.Name)
	}

	// Determine which tool to use
	tool := shareFlags.tool
	if tool == "" {
		if CommandExists("cloudflared") {
			tool = "cloudflared"
		} else if CommandExists("ngrok") {
			tool = "ngrok"
		} else {
			return fmt.Errorf("no tunnel tool found. Install cloudflared or ngrok")
		}
	}

	url := "https://" + s.Domain
	ui.Info("Sharing %s via %s...", s.Name, tool)
	ui.Dim("Press Ctrl+C to stop sharing")
	ui.Blank()

	switch tool {
	case "cloudflared":
		return runShareCloudflared(url)
	case "ngrok":
		return runShareNgrok(s.Domain)
	default:
		return fmt.Errorf("unsupported tunnel tool: %s", tool)
	}
}

func runShareCloudflared(url string) error {
	cmd := exec.Command("cloudflared", "tunnel", "--url", url)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runShareNgrok(domain string) error {
	// ngrok needs to connect to the local port
	cmd := exec.Command("ngrok", "http", "https://"+domain)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
