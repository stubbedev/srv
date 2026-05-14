// Package cmd — internal_http.go implements `srv internal` for toggling the
// plain-HTTP `internal` Traefik listener on a per-site basis.
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/ui"
)

var internalCmd = &cobra.Command{
	Use:   "internal",
	Short: "Manage the plain-HTTP internal listener (port 88) for a site",
	Long: `The 'internal' Traefik entrypoint exposes a site over plain HTTP on port 88
in addition to its normal HTTPS routing. Used for container-to-host calls that
need to skip TLS verification (e.g. server-side fetches from another container
on the same machine).`,
}

var internalEnableCmd = &cobra.Command{
	Use:   "enable SITE",
	Short: "Enable the internal listener on a site",
	Args:  cobra.ExactArgs(1),
	RunE:  runInternalEnable,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
}

var internalDisableCmd = &cobra.Command{
	Use:   "disable SITE",
	Short: "Disable the internal listener on a site",
	Args:  cobra.ExactArgs(1),
	RunE:  runInternalDisable,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
}

var internalListCmd = &cobra.Command{
	Use:   "list",
	Short: "List sites with the internal listener enabled",
	Args:  cobra.NoArgs,
	RunE:  runInternalList,
}

func init() {
	internalCmd.GroupID = GroupSites
	internalCmd.AddCommand(internalEnableCmd, internalDisableCmd, internalListCmd)
	RootCmd.AddCommand(internalCmd)
}

func runInternalEnable(cmd *cobra.Command, args []string) error {
	siteName := args[0]
	meta, err := site.ReadSiteMetadata(siteName)
	if err != nil {
		return err
	}
	if meta == nil {
		return fmt.Errorf("site not found: %s", siteName)
	}
	if site.HasListener(meta.Listeners, constants.ListenerInternal) {
		ui.Dim("Site %s already has the internal listener enabled", siteName)
		return nil
	}
	meta.Listeners = append(meta.Listeners, constants.ListenerInternal)
	if err := site.WriteSiteMetadata(siteName, *meta); err != nil {
		return fmt.Errorf("failed to update site metadata: %w", err)
	}
	if err := regenerateSiteRouting(siteName, meta); err != nil {
		ui.Warn("Failed to refresh routing config: %v", err)
	}
	ui.Success("Enabled internal listener for %s (http://%s:%d)", siteName, meta.PrimaryDomain(), constants.PortInternal)
	ui.Dim("Run `srv restart %s` to apply.", siteName)
	return nil
}

func runInternalDisable(cmd *cobra.Command, args []string) error {
	siteName := args[0]
	meta, err := site.ReadSiteMetadata(siteName)
	if err != nil {
		return err
	}
	if meta == nil {
		return fmt.Errorf("site not found: %s", siteName)
	}
	filtered := meta.Listeners[:0]
	removed := false
	for _, l := range meta.Listeners {
		if l == constants.ListenerInternal {
			removed = true
			continue
		}
		filtered = append(filtered, l)
	}
	if !removed {
		ui.Dim("Site %s does not have the internal listener enabled", siteName)
		return nil
	}
	meta.Listeners = filtered
	if err := site.WriteSiteMetadata(siteName, *meta); err != nil {
		return fmt.Errorf("failed to update site metadata: %w", err)
	}
	if err := regenerateSiteRouting(siteName, meta); err != nil {
		ui.Warn("Failed to refresh routing config: %v", err)
	}
	ui.Success("Disabled internal listener for %s", siteName)
	ui.Dim("Run `srv restart %s` to apply.", siteName)
	return nil
}

func runInternalList(cmd *cobra.Command, args []string) error {
	sites, err := site.List()
	if err != nil {
		return err
	}
	found := false
	for _, s := range sites {
		meta, err := site.ReadSiteMetadata(s.Name)
		if err != nil || meta == nil {
			continue
		}
		if site.HasListener(meta.Listeners, constants.ListenerInternal) {
			ui.Print("  %s  →  http://%s:%d", s.Name, meta.PrimaryDomain(), constants.PortInternal)
			found = true
		}
	}
	if !found {
		ui.Dim("No sites have the internal listener enabled.")
	}
	return nil
}
