package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/ui"
)

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
	pathsCmd.GroupID = GroupSystem
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
	ui.Print("  Local certs:     %s/*/certs/", cfg.SitesDir)
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
	Use:   "share SITE",
	Short: "Share a site via tunnel",
	Long: `Share a local site publicly using a tunnel service.

Supported tools:
  - cloudflared (Cloudflare Tunnel) - recommended
  - ngrok`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			cmd.Help()
			os.Exit(1)
		}
		return nil
	},
	RunE: runShare,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	shareCmd.Flags().StringVar(&shareFlags.tool, "tool", "", "Tunnel tool to use (cloudflared, ngrok)")
	shareCmd.GroupID = GroupSites
	RootCmd.AddCommand(shareCmd)
}

func runShare(cmd *cobra.Command, args []string) error {
	s, err := site.Get(args[0])
	if err != nil {
		return err
	}

	if s.Domain == "" {
		return fmt.Errorf("site '%s' has no domain configured", s.Name)
	}

	// Determine which tool to use
	tool := shareFlags.tool
	if tool == "" {
		if CommandExists(constants.ToolCloudflared) {
			tool = constants.ToolCloudflared
		} else if CommandExists(constants.ToolNgrok) {
			tool = constants.ToolNgrok
		} else {
			return fmt.Errorf("no tunnel tool found. Install cloudflared or ngrok")
		}
	}

	url := constants.SchemeHTTPSPrefix + s.Domain
	ui.Info("Sharing %s via %s...", s.Name, tool)
	ui.Dim("Press Ctrl+C to stop sharing")
	ui.Blank()

	switch tool {
	case constants.ToolCloudflared:
		return runShareCloudflared(url)
	case constants.ToolNgrok:
		return runShareNgrok(s.Domain)
	default:
		return fmt.Errorf("unsupported tunnel tool: %s", tool)
	}
}

func runShareCloudflared(url string) error {
	cmd := exec.Command(constants.ToolCloudflared, "tunnel", "--url", url)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runShareNgrok(domain string) error {
	// ngrok needs to connect to the local port
	cmd := exec.Command(constants.ToolNgrok, "http", constants.SchemeHTTPSPrefix+domain)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
