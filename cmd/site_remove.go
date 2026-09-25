// Package cmd — site_remove.go implements `srv remove`: stop a site's
// containers, drop its Traefik config, and clean up its config directory.
package cmd

import (
	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/ui"
)

// =============================================================================
// remove command
// =============================================================================

var removeFlags struct {
	yes bool
}

var removeCmd = &cobra.Command{
	Use:     "remove SITE",
	Aliases: []string{"rm"},
	Short:   "Remove a site",
	Long: `Stop a site's containers and remove it from srv.

This stops the site's containers, removes its Traefik routing, local
certificate, and DNS registrations, and deletes its srv config directory
(metadata and generated compose file). Your project directory on disk is
NOT touched.

Pass --yes to skip the confirmation gate (required for non-interactive runs).`,
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
	removeCmd.Flags().BoolVarP(&removeFlags.yes, "yes", "y", false, "Skip the confirmation gate (required for non-interactive runs)")
	removeCmd.GroupID = GroupSites
	RootCmd.AddCommand(removeCmd)
}

func runRemove(cmd *cobra.Command, args []string) error {
	siteName := args[0]

	// Confirmation gate, matching uninstall and install --fresh: without
	// --yes the command refuses so a fat-fingered 'srv rm' cannot silently
	// stop containers and delete the site's srv config.
	if !removeFlags.yes {
		ui.Warn("This will remove site '%s':", siteName)
		ui.Blank()
		ui.Print("  - Stop its containers")
		ui.Print("  - Remove its Traefik routing and local HTTPS certificate")
		ui.Print("  - Remove its DNS registrations")
		ui.Print("  - Delete its srv config directory (metadata, generated compose)")
		ui.Blank()
		ui.Dim("Your project directory on disk will NOT be removed.")
		return ui.UsageError("srv remove SITE --yes", "refusing to remove without --yes")
	}

	// Orchestration is shared with the MCP remove_site tool (internal/site).
	ui.Info("Removing %s...", siteName)
	warnings, err := site.RemoveSite(siteName)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		ui.Warn("%s", w)
	}
	ui.Success("Site '%s' removed", siteName)
	return nil
}
