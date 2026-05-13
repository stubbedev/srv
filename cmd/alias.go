// Package cmd — alias.go implements `srv alias` for managing extra hostnames
// mapped to a single site.
package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

var aliasCmd = &cobra.Command{
	Use:   "alias",
	Short: "Manage extra hostnames for a site",
	Long: `Each site has one canonical domain plus zero or more aliases. All hostnames
share a single set of generated configs (cert, DNS, Traefik router) so that
multiple hostnames route into the same container.`,
}

var aliasAddCmd = &cobra.Command{
	Use:   "add SITE DOMAIN",
	Short: "Add an alias hostname to a site",
	Args:  cobra.ExactArgs(2),
	RunE:  runAliasAdd,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
}

var aliasRemoveCmd = &cobra.Command{
	Use:     "remove SITE DOMAIN",
	Aliases: []string{"rm"},
	Short:   "Remove an alias hostname from a site",
	Args:    cobra.ExactArgs(2),
	RunE:    runAliasRemove,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
		}
		if len(args) == 1 {
			return GetSiteAliases(args[0]), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
}

var aliasListCmd = &cobra.Command{
	Use:   "list SITE",
	Short: "List a site's canonical domain and aliases",
	Args:  cobra.ExactArgs(1),
	RunE:  runAliasList,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	aliasCmd.GroupID = GroupSites
	aliasCmd.AddCommand(aliasAddCmd, aliasRemoveCmd, aliasListCmd)
	RootCmd.AddCommand(aliasCmd)
}

func runAliasAdd(cmd *cobra.Command, args []string) error {
	siteName := args[0]
	alias := strings.ToLower(strings.TrimSpace(args[1]))

	if err := ValidateDomain(alias); err != nil {
		return fmt.Errorf("invalid alias: %w", err)
	}

	meta, err := site.ReadSiteMetadata(siteName)
	if err != nil {
		return err
	}
	if meta == nil {
		return fmt.Errorf("site not found: %s", siteName)
	}
	if len(meta.Domains) == 0 {
		return fmt.Errorf("site %s has no canonical domain", siteName)
	}

	for _, d := range meta.Domains {
		if d == alias {
			ui.Dim("Alias %q already configured for %s", alias, siteName)
			return nil
		}
	}

	meta.Domains = append(meta.Domains, alias)
	if err := site.WriteSiteMetadata(siteName, *meta); err != nil {
		return fmt.Errorf("failed to update site metadata: %w", err)
	}

	if meta.IsLocal {
		if err := traefik.RegisterLocalDomain(alias, meta.Wildcard); err != nil {
			ui.Warn("Failed to register DNS for %s: %v", alias, err)
		}
		generateLocalCert(siteName, meta.Domains, meta.Wildcard)
	}

	if err := regenerateSiteRouting(siteName, meta); err != nil {
		ui.Warn("Failed to refresh routing config: %v", err)
	}

	ui.Success("Added alias %s → %s", alias, siteName)
	ui.Dim("Run `srv restart %s` to apply the new routing.", siteName)
	return nil
}

func runAliasRemove(cmd *cobra.Command, args []string) error {
	siteName := args[0]
	alias := strings.ToLower(strings.TrimSpace(args[1]))

	meta, err := site.ReadSiteMetadata(siteName)
	if err != nil {
		return err
	}
	if meta == nil {
		return fmt.Errorf("site not found: %s", siteName)
	}
	if len(meta.Domains) > 0 && meta.Domains[0] == alias {
		return fmt.Errorf("%s is the canonical domain — remove the site to drop it", alias)
	}

	filtered := meta.Domains[:0]
	removed := false
	for _, d := range meta.Domains {
		if d == alias {
			removed = true
			continue
		}
		filtered = append(filtered, d)
	}
	if !removed {
		return fmt.Errorf("alias %q is not registered for %s", alias, siteName)
	}
	meta.Domains = filtered

	if err := site.WriteSiteMetadata(siteName, *meta); err != nil {
		return fmt.Errorf("failed to update site metadata: %w", err)
	}

	if meta.IsLocal {
		if err := traefik.UnregisterLocalDomain(alias); err != nil {
			ui.Warn("Failed to unregister DNS for %s: %v", alias, err)
		}
		// Regenerate cert without the dropped alias.
		generateLocalCert(siteName, meta.Domains, meta.Wildcard)
	}

	if err := regenerateSiteRouting(siteName, meta); err != nil {
		ui.Warn("Failed to refresh routing config: %v", err)
	}

	ui.Success("Removed alias %s from %s", alias, siteName)
	ui.Dim("Run `srv restart %s` to apply the routing change.", siteName)
	return nil
}

func runAliasList(cmd *cobra.Command, args []string) error {
	siteName := args[0]
	meta, err := site.ReadSiteMetadata(siteName)
	if err != nil {
		return err
	}
	if meta == nil {
		return fmt.Errorf("site not found: %s", siteName)
	}
	if len(meta.Domains) == 0 {
		ui.Dim("Site %s has no domains configured", siteName)
		return nil
	}

	ui.Print("  Canonical: %s", meta.Domains[0])
	if len(meta.Domains) == 1 {
		ui.Dim("  (no aliases)")
		return nil
	}
	for _, alias := range meta.Domains[1:] {
		ui.Print("  Alias:     %s", alias)
	}
	return nil
}

// regenerateSiteRouting rewrites the Traefik file-provider config for a
// compose-type site after its domain set changes. Container-label sites
// (PHP/static/node/...) need a restart to pick up the new label-derived rule;
// the caller surfaces that hint to the user.
func regenerateSiteRouting(siteName string, meta *site.SiteMetadata) error {
	if meta.Type != site.SiteTypeCompose {
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return traefik.WriteSiteRouteConfig(cfg, traefik.SiteRouteConfig{
		Name:        siteName,
		Domains:     meta.Domains,
		ServiceName: meta.ServiceName,
		Port:        meta.Port,
		IsLocal:     meta.IsLocal,
		Wildcard:    meta.Wildcard,
		Listeners:   meta.Listeners,
	})
}
