// Package cmd — network.go implements `srv network` for attaching a site's
// container(s) to additional external Docker networks. Used to reach
// user-managed service containers (MySQL, Redis, Elasticsearch, …) by their
// container name from inside the FrankenPHP container.
package cmd

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/ui"
)

var networkCmd = &cobra.Command{
	Use:   "network",
	Short: "Manage extra Docker networks attached to a site",
	Long: `Attach a site's container(s) to additional external Docker networks so PHP
code (or any other in-container process) can reach user-managed containers
by name.

Typical use: you run MySQL/Redis/Elasticsearch via your own docker-compose
elsewhere, and want srv-managed sites to talk to those containers by their
container hostname (e.g. DB_HOST=mysql01) without falling back to
host.docker.internal.`,
}

var networkAttachCmd = &cobra.Command{
	Use:   "attach SITE NETWORK",
	Short: "Attach a site's container to an external Docker network",
	Args:  cobra.ExactArgs(2),
	RunE:  runNetworkAttach,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
}

var networkDetachCmd = &cobra.Command{
	Use:     "detach SITE NETWORK",
	Aliases: []string{"remove", "rm"},
	Short:   "Detach a site from an external Docker network",
	Args:    cobra.ExactArgs(2),
	RunE:    runNetworkDetach,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
		}
		if len(args) == 1 {
			return GetSiteExtraNetworks(args[0]), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
}

var networkListCmd = &cobra.Command{
	Use:   "list SITE",
	Short: "List extra Docker networks attached to a site",
	Args:  cobra.ExactArgs(1),
	RunE:  runNetworkList,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	networkCmd.GroupID = GroupSites
	networkCmd.AddCommand(networkAttachCmd, networkDetachCmd, networkListCmd)
	RootCmd.AddCommand(networkCmd)
}

func runNetworkAttach(cmd *cobra.Command, args []string) error {
	siteName, network := args[0], strings.TrimSpace(args[1])
	if network == "" {
		return fmt.Errorf("network name is required")
	}

	meta, err := site.ReadSiteMetadata(siteName)
	if err != nil {
		return err
	}
	if meta == nil {
		return fmt.Errorf("site not found: %s", siteName)
	}

	if !docker.NetworkExists(network) {
		return fmt.Errorf("docker network %q does not exist — create it first (or check the name)", network)
	}

	if slices.Contains(meta.ExtraNetworks, network) {
		ui.Dim("%s is already attached to %s", network, siteName)
		return nil
	}
	if network == meta.NetworkName {
		return fmt.Errorf("%q is the site's primary traefik network — already attached", network)
	}

	meta.ExtraNetworks = append(meta.ExtraNetworks, network)
	sort.Strings(meta.ExtraNetworks)

	if err := site.WriteSiteMetadata(siteName, *meta); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	if _, err := site.Reload(siteName); err != nil {
		ui.Warn("Failed to refresh site config: %v", err)
	}

	ui.Success("Attached %s to %s", network, siteName)
	ui.Dim("Run 'srv restart %s' for the container to pick up the new network.", siteName)
	return nil
}

func runNetworkDetach(cmd *cobra.Command, args []string) error {
	siteName, network := args[0], strings.TrimSpace(args[1])

	meta, err := site.ReadSiteMetadata(siteName)
	if err != nil {
		return err
	}
	if meta == nil {
		return fmt.Errorf("site not found: %s", siteName)
	}

	idx := slices.Index(meta.ExtraNetworks, network)
	if idx < 0 {
		return fmt.Errorf("network %q not attached to %s", network, siteName)
	}
	meta.ExtraNetworks = slices.Delete(meta.ExtraNetworks, idx, idx+1)

	if err := site.WriteSiteMetadata(siteName, *meta); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	if _, err := site.Reload(siteName); err != nil {
		ui.Warn("Failed to refresh site config: %v", err)
	}

	ui.Success("Detached %s from %s", network, siteName)
	ui.Dim("Run 'srv restart %s' for the change to take effect.", siteName)
	return nil
}

func runNetworkList(cmd *cobra.Command, args []string) error {
	siteName := args[0]
	meta, err := site.ReadSiteMetadata(siteName)
	if err != nil {
		return err
	}
	if meta == nil {
		return fmt.Errorf("site not found: %s", siteName)
	}
	ui.Print("Primary: %s", meta.NetworkName)
	if len(meta.ExtraNetworks) == 0 {
		ui.Dim("No extra networks attached to %s", siteName)
		return nil
	}
	for _, n := range meta.ExtraNetworks {
		ui.Print("  %s", n)
	}
	return nil
}

// GetSiteExtraNetworks returns the extra networks attached to a site, for
// shell completion. Returns nil on lookup error.
func GetSiteExtraNetworks(siteName string) []string {
	meta, err := site.ReadSiteMetadata(siteName)
	if err != nil || meta == nil {
		return nil
	}
	return append([]string(nil), meta.ExtraNetworks...)
}
