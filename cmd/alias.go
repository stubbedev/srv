// Package cmd — alias.go implements `srv alias` for managing extra hostnames
// mapped to a single site.
package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/site"
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

	// Orchestration is shared with the MCP add_alias tool (internal/site).
	changed, warnings, err := site.AddAlias(siteName, alias)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		ui.Warn("%s", w)
	}
	if !changed {
		ui.Dim("Alias %q already configured for %s", alias, siteName)
		return nil
	}
	ui.Success("Added alias %s → %s", alias, siteName)
	ui.Dim("Run `srv restart %s` to apply the new routing.", siteName)
	return nil
}

func runAliasRemove(cmd *cobra.Command, args []string) error {
	siteName := args[0]
	alias := strings.ToLower(strings.TrimSpace(args[1]))

	warnings, err := site.RemoveAlias(siteName, alias)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		ui.Warn("%s", w)
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
