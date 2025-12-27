package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

// =============================================================================
// open command
// =============================================================================

var openCmd = &cobra.Command{
	Use:   "open [SITE]",
	Short: "Open site in browser",
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
// paths command
// =============================================================================

var pathsCmd = &cobra.Command{
	Use:   "paths",
	Short: "Show config paths",
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
	Short: "Share site via tunnel",
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
