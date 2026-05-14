// Package cmd — site_runtime.go groups the config-management commands:
// `srv runtime`, `srv regenerate`, and `srv edit`.
package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/ui"
)

// =============================================================================
// runtime command
// =============================================================================

var runtimeFlags struct {
	phpVersion    string
	phpExtensions string
	nodeVersion   string
}

var runtimeCmd = &cobra.Command{
	Use:   "runtime SITE",
	Short: "Change runtime version or PHP extensions for a site",
	Long: `Update the runtime version or PHP extensions for a PHP or Node.js site.

For PHP sites the Dockerfile is rebuilt with the new version / extension list.
php.ini and nginx.conf are left untouched so your customisations are preserved.

For Node.js sites the docker-compose.yml is regenerated with the new version.

After updating, the site containers are rebuilt and restarted automatically.

Examples:
  srv site runtime mysite --php-version 8.3
  srv site runtime mysite --php-extensions "+redis,-xdebug"
  srv site runtime mysite --node-version 22`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			_ = cmd.Help()
			return ui.UsageError("srv site runtime SITE", "a site name is required")
		}
		if len(args) > 1 {
			return ui.UsageError("srv site runtime SITE", "too many arguments — expected a single site name, got %d", len(args))
		}
		return nil
	},
	RunE: runRuntime,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	runtimeCmd.Flags().StringVar(&runtimeFlags.phpVersion, "php-version", "", "PHP version (e.g. 8.3, 8.2, latest)")
	runtimeCmd.Flags().StringVar(&runtimeFlags.phpExtensions, "php-extensions", "", "PHP extensions: full list, or +ext/-ext to add/remove from defaults")
	runtimeCmd.Flags().StringVar(&runtimeFlags.nodeVersion, "node-version", "", "Node.js version (e.g. 22, 20, lts)")
	_ = runtimeCmd.RegisterFlagCompletionFunc("php-extensions", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return site.KnownPHPExtensions(), cobra.ShellCompDirectiveNoFileComp
	})
	runtimeCmd.GroupID = GroupSites
	RootCmd.AddCommand(runtimeCmd)
}

func runRuntime(cmd *cobra.Command, args []string) error {
	siteName := args[0]

	meta, err := site.ReadSiteMetadata(siteName)
	if err != nil || meta == nil {
		return fmt.Errorf("site '%s' not found", siteName)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	switch meta.Type {
	case site.SiteTypePHP:
		return runtimeUpdatePHP(cfg, siteName, meta)
	case site.SiteTypeNode:
		return runtimeUpdateNode(cfg, siteName, meta)
	default:
		return fmt.Errorf("site '%s' is a %s site — runtime management is only available for PHP and Node.js sites", siteName, meta.Type)
	}
}

func runtimeUpdatePHP(cfg *config.Config, siteName string, meta *site.SiteMetadata) error {
	if runtimeFlags.phpVersion == "" && runtimeFlags.phpExtensions == "" {
		return fmt.Errorf("specify at least --php-version or --php-extensions")
	}

	// Show before/after diff.
	if runtimeFlags.phpVersion != "" && runtimeFlags.phpVersion != meta.PHPVersion {
		ui.Dim("  php version:  %s → %s", meta.PHPVersion, runtimeFlags.phpVersion)
		meta.PHPVersion = runtimeFlags.phpVersion
	}
	if runtimeFlags.phpExtensions != "" {
		before := len(meta.PHPExtensions)
		meta.PHPExtensions = site.ParseExtensionOverrides(runtimeFlags.phpExtensions, meta.PHPExtensions)
		after := len(meta.PHPExtensions)
		if before != after {
			ui.Dim("  extensions:   %d → %d", before, after)
		}
	}

	phpInfo := &site.PHPSiteInfo{
		PHPVersion:   meta.PHPVersion,
		Extensions:   meta.PHPExtensions,
		Framework:    meta.PHPFramework,
		DocumentRoot: meta.DocumentRoot,
	}

	ui.Info("Updating PHP runtime for '%s'...", siteName)
	if err := site.WritePHPDockerConfig(siteName, *meta, phpInfo); err != nil {
		return fmt.Errorf("failed to write PHP config: %w", err)
	}

	// Persist updated metadata.
	if err := site.WriteSiteMetadata(siteName, *meta); err != nil {
		return fmt.Errorf("failed to update metadata: %w", err)
	}

	composeDir := site.SiteConfigDir(cfg, siteName)
	ui.Info("Rebuilding and restarting containers...")
	if err := docker.ComposeUpBuildWithProfile(composeDir, meta.Profile); err != nil {
		return fmt.Errorf("failed to rebuild containers: %w", err)
	}

	ui.Success("PHP runtime updated for '%s' (version: %s)", siteName, meta.PHPVersion)
	return nil
}

func runtimeUpdateNode(cfg *config.Config, siteName string, meta *site.SiteMetadata) error {
	if runtimeFlags.nodeVersion == "" {
		return fmt.Errorf("specify --node-version")
	}

	if meta.NodeRuntime != "" && meta.NodeRuntime != constants.NodeRuntimeNode {
		return fmt.Errorf("--node-version only applies to Node.js sites (this site uses %s)", meta.NodeRuntime)
	}

	if runtimeFlags.nodeVersion != meta.NodeVersion {
		ui.Dim("  node version:  %s → %s", meta.NodeVersion, runtimeFlags.nodeVersion)
	}
	meta.NodeVersion = runtimeFlags.nodeVersion

	nodeInfo := &site.NodeSiteInfo{
		Runtime:        meta.NodeRuntime,
		PackageManager: meta.NodePackageManager,
		NodeVersion:    meta.NodeVersion,
		Framework:      meta.NodeFramework,
		StartCmd:       meta.NodeStartCmd,
		Port:           meta.Port,
	}

	ui.Info("Updating Node.js runtime for '%s'...", siteName)
	if err := site.WriteNodeSiteConfig(siteName, *meta, nodeInfo, true); err != nil {
		return fmt.Errorf("failed to write Node config: %w", err)
	}

	// Persist updated metadata.
	if err := site.WriteSiteMetadata(siteName, *meta); err != nil {
		return fmt.Errorf("failed to update metadata: %w", err)
	}

	composeDir := site.SiteConfigDir(cfg, siteName)
	ui.Info("Restarting containers...")
	if err := docker.ComposeUpWithProfile(composeDir, meta.Profile); err != nil {
		return fmt.Errorf("failed to restart containers: %w", err)
	}

	ui.Success("Node.js runtime updated for '%s' (version: %s)", siteName, meta.NodeVersion)
	return nil
}

// =============================================================================
// regenerate command
// =============================================================================

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

	// Confirm with user before overwriting.
	var confirmed bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Regenerate config for '%s'?", siteName)).
				Description("This will overwrite nginx.conf, php.ini, docker-compose.yml, and Dockerfile.\nYour manual edits will be lost.").
				Value(&confirmed),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}
	if !confirmed {
		ui.Dim("Cancelled.")
		return nil
	}

	ui.Info("Regenerating config for '%s'...", siteName)

	switch meta.Type {
	case site.SiteTypePHP:
		phpInfo := &site.PHPSiteInfo{
			PHPVersion:   meta.PHPVersion,
			Extensions:   meta.PHPExtensions,
			Framework:    meta.PHPFramework,
			DocumentRoot: meta.DocumentRoot,
		}
		if err := site.WritePHPSiteConfig(siteName, *meta, phpInfo, true); err != nil {
			return fmt.Errorf("failed to regenerate PHP config: %w", err)
		}
	case site.SiteTypeNode:
		nodeInfo := &site.NodeSiteInfo{
			Runtime:        meta.NodeRuntime,
			PackageManager: meta.NodePackageManager,
			NodeVersion:    meta.NodeVersion,
			Framework:      meta.NodeFramework,
			StartCmd:       meta.NodeStartCmd,
			Port:           meta.Port,
		}
		if err := site.WriteNodeSiteConfig(siteName, *meta, nodeInfo, true); err != nil {
			return fmt.Errorf("failed to regenerate Node config: %w", err)
		}
	case site.SiteTypeRuby:
		rubyInfo := &site.RubySiteInfo{
			RubyVersion: meta.RubyVersion,
			Framework:   meta.RubyFramework,
			StartCmd:    meta.RubyStartCmd,
			Port:        meta.Port,
		}
		if err := site.WriteRubySiteConfig(siteName, *meta, rubyInfo, true); err != nil {
			return fmt.Errorf("failed to regenerate Ruby config: %w", err)
		}
	case site.SiteTypePython:
		pythonInfo := &site.PythonSiteInfo{
			PythonVersion: meta.PythonVersion,
			Framework:     meta.PythonFramework,
			StartCmd:      meta.PythonStartCmd,
			Port:          meta.Port,
		}
		if err := site.WritePythonSiteConfig(siteName, *meta, pythonInfo, true); err != nil {
			return fmt.Errorf("failed to regenerate Python config: %w", err)
		}
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
		return fmt.Errorf("regenerate is only available for PHP, Node.js, Ruby, Python, Dockerfile, and static sites")
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
