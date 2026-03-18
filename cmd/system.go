package cmd

import (
	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
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
