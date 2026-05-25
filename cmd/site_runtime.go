// Package cmd — site_runtime.go groups the config-management commands:
// `srv regenerate` and `srv edit`. (The `srv runtime` command is gone:
// srv no longer manages language runtime versions — that's the user's
// Dockerfile.)
package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/ui"
)

// =============================================================================
// regenerate command
// =============================================================================

var regenerateFlags struct {
	force bool
}

var regenerateCmd = &cobra.Command{
	Use:   "regenerate SITE",
	Short: "Regenerate config files for a site, overwriting any customisations",
	Long: `Regenerate all config files (nginx.conf, php.ini, docker-compose.yml, Dockerfile)
for a site from scratch, overwriting any manual edits.

Use this if you want to reset your config to srv defaults, or after changing
the domain / SSL type.

The site will be restarted automatically after regeneration.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			_ = cmd.Help()
			return ui.UsageError("srv site regenerate SITE", "a site name is required")
		}
		if len(args) > 1 {
			return ui.UsageError("srv site regenerate SITE", "too many arguments — expected a single site name, got %d", len(args))
		}
		return nil
	},
	RunE: runRegenerate,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	regenerateCmd.Flags().BoolVarP(&regenerateFlags.force, "force", "f", false, "Required: confirm overwriting any manual edits to nginx.conf / Dockerfile / compose.yml")
	regenerateCmd.GroupID = GroupSites
	RootCmd.AddCommand(regenerateCmd)
}

func runRegenerate(cmd *cobra.Command, args []string) error {
	siteName := args[0]

	meta, err := site.ReadSiteMetadata(siteName)
	if err != nil || meta == nil {
		return fmt.Errorf("site '%s' not found", siteName)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Gate behind --force so a script can't blow away manual edits by accident.
	if !regenerateFlags.force {
		return fmt.Errorf("regenerate refused: manual edits to nginx.conf / Dockerfile / docker-compose.yml will be overwritten — re-run with --force to proceed")
	}

	ui.Info("Regenerating config for '%s'...", siteName)

	switch meta.Type {
	case site.SiteTypeDockerfile:
		dockerfileInfo := &site.DockerfileSiteInfo{Port: meta.DockerfilePort}
		if err := site.WriteDockerfileSiteConfig(siteName, *meta, dockerfileInfo, true); err != nil {
			return fmt.Errorf("failed to regenerate Dockerfile config: %w", err)
		}
	case site.SiteTypeStatic:
		if err := site.WriteStaticSiteConfig(siteName, *meta, true); err != nil {
			return fmt.Errorf("failed to regenerate static config: %w", err)
		}
	default:
		return fmt.Errorf("regenerate is only available for dockerfile and static sites; language runtimes (Node/Ruby/Python/PHP) are user-owned now — edit your Dockerfile or re-run `srv scaffold`")
	}

	composeDir := site.SiteConfigDir(cfg, siteName)
	ui.Info("Restarting containers...")
	if err := docker.ComposeUpBuildWithProfile(composeDir, meta.Profile); err != nil {
		return fmt.Errorf("failed to restart containers: %w", err)
	}

	ui.Success("Config regenerated for '%s'", siteName)
	return nil
}

// =============================================================================
// edit command
// =============================================================================

var editCmd = &cobra.Command{
	Use:   "edit SITE",
	Short: "Open the config directory for a site in your editor",
	Long: `Open the srv config directory for a site in $EDITOR (or $VISUAL).

The config directory contains nginx.conf, php.ini, docker-compose.yml,
and the Dockerfile — all of which you can freely edit.

If no editor is configured, the path is printed so you can open it manually.

After editing, run "srv site restart SITE" for changes to take effect.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			_ = cmd.Help()
			return ui.UsageError("srv site edit SITE", "a site name is required")
		}
		if len(args) > 1 {
			return ui.UsageError("srv site edit SITE", "too many arguments — expected a single site name, got %d", len(args))
		}
		return nil
	},
	RunE: runEdit,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	editCmd.GroupID = GroupSites
	RootCmd.AddCommand(editCmd)
}

func runEdit(cmd *cobra.Command, args []string) error {
	siteName := args[0]

	if !site.Exists(siteName) {
		return fmt.Errorf("site '%s' not found", siteName)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	siteDir := site.SiteConfigDir(cfg, siteName)

	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}

	if editor == "" {
		ui.Print("Config directory for '%s':", siteName)
		ui.Print("  %s", siteDir)
		ui.Blank()
		ui.Dim("Set $EDITOR or $VISUAL to open it automatically.")
		ui.Dim("After editing, run: srv site restart %s", siteName)
		return nil
	}

	ui.Dim("Opening %s in %s...", siteDir, editor)
	c := exec.Command(editor, siteDir) //nolint:gosec // editor from trusted env var
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("editor exited with error: %w", err)
	}

	ui.Dim("Run 'srv site restart %s' for changes to take effect.", siteName)
	return nil
}
