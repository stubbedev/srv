// Package cmd — reload.go implements `srv reload` which re-applies a site's
// metadata.yml without restarting the container by default. Same code path
// the daemon hot-reload uses.
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/ui"
)

var reloadFlags struct {
	all     bool
	restart bool
}

var reloadCmd = &cobra.Command{
	Use:   "reload [SITE]",
	Short: "Re-apply a site's metadata.yml without restarting (unless --restart)",
	Long: `Re-applies generated artifacts (nginx.conf, Traefik routing, certs, DNS)
from the site's metadata.yml.

Compose-type sites pick up routing changes via Traefik's file provider
without a restart. Srv-managed sites (php/static/node/...) need a
container restart to apply changes baked into Docker labels — pass
--restart to do that in one step.`,
	RunE: runReload,
	Args: func(cmd *cobra.Command, args []string) error {
		if reloadFlags.all {
			return cobra.NoArgs(cmd, args)
		}
		return cobra.ExactArgs(1)(cmd, args)
	},
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	reloadCmd.Flags().BoolVarP(&reloadFlags.all, "all", "a", false, "Reload all registered sites")
	reloadCmd.Flags().BoolVar(&reloadFlags.restart, "restart", false, "Restart the site's container after reload (required for label-based sites to pick up changes)")
	reloadCmd.GroupID = GroupSites
	RootCmd.AddCommand(reloadCmd)
}

func runReload(cmd *cobra.Command, args []string) error {
	var names []string
	if reloadFlags.all {
		names = append(names, GetSiteNames()...)
	} else {
		names = []string{args[0]}
	}

	if len(names) == 0 {
		ui.Dim("No sites registered")
		return nil
	}

	var firstErr error
	for _, name := range names {
		if err := reloadOne(name); err != nil {
			ui.Warn("%s: %v", name, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func reloadOne(name string) error {
	res, err := site.Reload(name)
	if err != nil {
		return err
	}
	for _, w := range res.Warnings {
		ui.Warn("%s: %s", name, w)
	}

	switch {
	case reloadFlags.restart:
		s, gerr := site.GetByName(name)
		if gerr != nil {
			return fmt.Errorf("lookup site: %w", gerr)
		}
		if s.IsBroken {
			return fmt.Errorf("site is broken (target directory missing)")
		}
		ui.Info("Restarting %s...", name)
		if err := docker.ComposeUpWithProfile(s.ComposeDir, s.Profile); err != nil {
			return fmt.Errorf("docker compose up: %w", err)
		}
		ui.Success("Reloaded and restarted %s", name)
	case res.NeedsRestart:
		ui.Success("Reloaded %s (run `srv restart %s` to apply container-level changes)", name, name)
	default:
		ui.Success("Reloaded %s", name)
	}
	return nil
}
