package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/ui"
)

var composerCmd = &cobra.Command{
	Use:   "composer [SITE]",
	Short: "Run composer inside a site's PHP container",
	Long: `Run composer commands inside the PHP container of a site.

The SITE argument is optional — if omitted, srv will detect the site from
the current directory.

Examples:
  srv composer install
  srv composer mysite require laravel/sanctum
  srv composer update --no-dev`,
	GroupID: GroupSites,
}

func init() {
	composerCmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	}
	RootCmd.AddCommand(composerCmd)

	for _, sub := range composerSubcommands() {
		composerCmd.AddCommand(sub)
	}
}

// resolveComposerSite resolves the target site from the first positional arg
// (if it matches a registered site name) or from the current working directory.
// It returns the site and any remaining args after the site name is consumed.
func resolveComposerSite(args []string) (*site.Site, []string, error) {
	if len(args) > 0 {
		if candidate, err := site.Get(args[0]); err == nil {
			return candidate, args[1:], nil
		}
	}
	s, err := GetSiteFromArgs(nil)
	if err != nil {
		return nil, args, err
	}
	if s == nil {
		return nil, args, fmt.Errorf("no site specified and current directory is not a registered site")
	}
	return s, args, nil
}

// runComposerCommand resolves the site, ensures it is running, then execs
// `composer <subcmd> <args...>` inside the PHP container.
// rawArgs should be the full os.Args tail after the composer subcommand name,
// so that flags like --no-dev are forwarded verbatim.
func runComposerCommand(subcmd string, rawArgs []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	s, extra, err := resolveComposerSite(rawArgs)
	if err != nil {
		return err
	}

	if s.Type != site.SiteTypePHP {
		return fmt.Errorf("site '%s' is not a PHP site", s.Name)
	}

	if s.Status != "running" {
		ui.Info("Starting site '%s'...", s.Name)
		if err := docker.ComposeUp(s.ComposeDir); err != nil {
			return fmt.Errorf("failed to start site '%s': %w", s.Name, err)
		}
	}

	containerName := "srv-" + s.Name + "-php"
	composerArgs := append([]string{subcmd}, extra...)
	return docker.Exec(containerName, append([]string{"composer"}, composerArgs...)...)
}

// composerSubcmd builds a composer subcommand that forwards all flags and args
// verbatim to composer inside the container.
func composerSubcmd(use, short string, aliases []string) *cobra.Command {
	// Extract just the command name (first word of use).
	name := use
	for i, c := range use {
		if c == ' ' {
			name = use[:i]
			break
		}
	}
	cmd := &cobra.Command{
		Use:                use,
		Short:              short,
		Aliases:            aliases,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runComposerCommand(name, args)
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			// First arg may be a site name.
			if len(args) == 0 {
				return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
	}
	return cmd
}

func composerSubcommands() []*cobra.Command {
	return []*cobra.Command{
		composerSubcmd(
			"install [SITE]",
			"Install dependencies from composer.lock",
			nil,
		),
		composerSubcmd(
			"update [SITE] [packages...]",
			"Update dependencies to their latest versions",
			[]string{"u", "upgrade"},
		),
		composerSubcmd(
			"require [SITE] [packages...]",
			"Add new packages to composer.json",
			[]string{"r"},
		),
		composerSubcmd(
			"remove [SITE] [packages...]",
			"Remove packages from composer.json",
			[]string{"rm", "uninstall"},
		),
		composerSubcmd(
			"init [SITE]",
			"Interactively create a composer.json in the current directory",
			nil,
		),
		composerSubcmd(
			"dump-autoload [SITE]",
			"Regenerate the autoloader",
			[]string{"dumpautoload"},
		),
		composerSubcmd(
			"validate [SITE]",
			"Validate composer.json and composer.lock",
			nil,
		),
		composerSubcmd(
			"show [SITE] [package]",
			"Show information about installed packages",
			[]string{"info"},
		),
		composerSubcmd(
			"outdated [SITE]",
			"Show packages that have updates available",
			nil,
		),
		composerSubcmd(
			"audit [SITE]",
			"Audit installed packages for security advisories",
			nil,
		),
		composerSubcmd(
			"run-script [SITE] [script]",
			"Run a script defined in composer.json",
			[]string{"run"},
		),
		composerSubcmd(
			"clear-cache [SITE]",
			"Clear the composer cache",
			[]string{"clearcache", "cc"},
		),
		composerSubcmd(
			"status [SITE]",
			"Show local changes in installed source packages",
			nil,
		),
		composerSubcmd(
			"bump [SITE]",
			"Increase lower version limits to installed versions",
			nil,
		),
		composerSubcmd(
			"reinstall [SITE] [packages...]",
			"Reinstall packages to get a clean copy",
			nil,
		),
		composerSubcmd(
			"depends [SITE] [package]",
			"Show which packages depend on a given package",
			[]string{"why"},
		),
		composerSubcmd(
			"prohibits [SITE] [package]",
			"Show what prevents a package from being installed",
			[]string{"why-not"},
		),
	}
}
