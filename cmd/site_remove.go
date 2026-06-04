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
