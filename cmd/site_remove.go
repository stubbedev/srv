// Package cmd — site_remove.go implements `srv remove`: stop a site's
// containers, drop its Traefik config, and clean up its config directory.
package cmd

import (
	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

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
