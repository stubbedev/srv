// Package cmd — site_add.go is the entry point for `srv add`: cobra wiring,
// the `siteSetup` struct that threads through the rest of the flow
// (site_add_detect/prompt/files/finalize), and the top-level orchestration.
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/ui"
)

// =============================================================================
// add command
// =============================================================================

var addFlags struct {
	domain         string
	aliases        []string
	port           int
	name           string
	service        string
	local          bool
	wildcard       bool
	internalHTTP   bool
	force          bool
	skipValidation bool
	typeOverride   string // Force site type: dockerfile/static/compose
	// Static site options
	spa   bool
	cache bool
	cors  bool
	// Compose profile selection
	profile string
	// Extra mounts
	volumes []string
}

var addCmd = &cobra.Command{
	Use:   "add PATH",
	Short: "Add a site",
	Long: `Register a new site with srv and generate Traefik configuration.

If the PATH contains a docker-compose.yml file, srv will configure Traefik
to route traffic to the specified service. No files are created in the
project directory - all config is stored in ~/.config/srv.

If no docker-compose.yml is found, srv will serve the directory as static
files using nginx.

SSL certificates:
  - Use --local to generate a local certificate with mkcert
  - Without --local, Let's Encrypt will be used for production SSL

Examples:
  srv add /path/to/site --domain example.com          # Production with Let's Encrypt
  srv add /path/to/site --domain myapp.test --local   # Local dev with mkcert
  srv add . --domain example.com --start              # Add and start immediately
  srv add /path/to/static --domain site.test --local  # Static files with nginx`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			_ = cmd.Help()
			return ui.UsageError("srv add PATH --domain DOMAIN", "a path to a directory is required")
		}
		if len(args) > 1 {
			return ui.UsageError("srv add PATH --domain DOMAIN", "too many arguments — expected a single directory path, got %d", len(args))
		}
		return nil
	},
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if addFlags.domain == "" {
			_ = cmd.Help()
			return ui.UsageError("srv add PATH --domain DOMAIN", "--domain is required (e.g. --domain myapp.test or --domain example.com)")
		}
		return nil
	},
	RunE: runAdd,
}

func init() {
	addCmd.Flags().StringVarP(&addFlags.domain, "domain", "d", "", "Domain/hostname (e.g., example.com or myapp.test)")
	addCmd.Flags().StringSliceVar(&addFlags.aliases, "alias", nil, "Additional hostname mapped to the same site (repeatable)")
	_ = addCmd.RegisterFlagCompletionFunc("alias", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveNoFileComp
	})
	addCmd.Flags().IntVarP(&addFlags.port, "port", "p", constants.DefaultContainerPort, "Container port")
	addCmd.Flags().StringVarP(&addFlags.name, "name", "n", "", "Site name (default: directory name)")
	addCmd.Flags().StringVar(&addFlags.service, "service", "", "Container name to route to")
	addCmd.Flags().BoolVarP(&addFlags.local, "local", "l", false, "Use local SSL via mkcert (otherwise Let's Encrypt)")
	addCmd.Flags().BoolVar(&addFlags.wildcard, "wildcard", false, "Also match one-level subdomains (e.g. *.foo.test); local sites only")
	addCmd.Flags().BoolVar(&addFlags.internalHTTP, "internal-http", false, "Expose the site on the internal plain-HTTP entrypoint (port 88) in addition to HTTPS")
	addCmd.Flags().BoolVarP(&addFlags.force, "force", "f", false, "Overwrite existing configuration")
	addCmd.Flags().BoolVar(&addFlags.skipValidation, "skip-validation", false, "Skip compose file validation")
	// Static site options
	addCmd.Flags().BoolVar(&addFlags.spa, "spa", true, "Enable SPA mode (fallback to index.html)")
	addCmd.Flags().BoolVar(&addFlags.cache, "cache", true, "Enable caching headers for static assets")
	addCmd.Flags().BoolVar(&addFlags.cors, "cors", false, "Enable CORS headers (allow all origins)")
	// Compose profile (required when the selected service has multiple)
	addCmd.Flags().StringVar(&addFlags.profile, "profile", "", "Docker Compose profile (required when the selected service declares multiple)")
	// Extra bind-mounts
	addCmd.Flags().StringSliceVar(&addFlags.volumes, "volume", nil, "Extra bind-mount in HOST:CONTAINER[:ro] form; repeatable")
	_ = addCmd.RegisterFlagCompletionFunc("volume", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveDefault
	})
	// Type override
	addCmd.Flags().StringVar(&addFlags.typeOverride, "type", "", "Force site type: dockerfile, static, compose")
	_ = addCmd.RegisterFlagCompletionFunc("type", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"dockerfile", "static", "compose"}, cobra.ShellCompDirectiveNoFileComp
	})
	addCmd.GroupID = GroupSites
	RootCmd.AddCommand(addCmd)
}

func runAdd(cmd *cobra.Command, args []string) error {
	// Parse the bind-mount flags here (CLI spec format); the rest of the add
	// pipeline lives in internal/site so the CLI and the MCP add_site tool
	// share one implementation.
	var mounts []site.VolumeMount
	for _, spec := range addFlags.volumes {
		m, err := ParseVolumeSpec(spec)
		if err != nil {
			return fmt.Errorf("invalid --volume %q: %w", spec, err)
		}
		mounts = append(mounts, m)
	}

	res, err := site.Add(site.AddOptions{
		Path:         args[0],
		TypeOverride: addFlags.typeOverride,
		Name:         addFlags.name,
		Domain:       addFlags.domain,
		Aliases:      addFlags.aliases,
		Port:         addFlags.port,
		Local:        addFlags.local,
		Wildcard:     addFlags.wildcard,
		InternalHTTP: addFlags.internalHTTP,
		Service:      addFlags.service,
		Profile:      addFlags.profile,
		SPA:          addFlags.spa,
		Cache:        addFlags.cache,
		CORS:         addFlags.cors,
		Volumes:      mounts,
		Force:        addFlags.force,
		Start:        true,
	})
	if err != nil {
		return err
	}
	for _, w := range res.Warnings {
		ui.Warn("%s", w)
	}

	ui.Success("Site '%s' added successfully!", res.Name)
	ui.Dim("Domain: %s (%s, %s)", res.Domain, res.Type, ui.Highlight(TypeLabel(res.IsLocal)))
	if cfg, err := config.Load(); err == nil {
		ui.Dim("Config: %s/sites/%s/ (no project files modified)", cfg.Root, res.Name)
	}
	if res.IsLocal {
		ui.Success("Site is running at https://%s", res.Domain)
	}
	return nil
}
