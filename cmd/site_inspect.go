// Package cmd — site_inspect.go bundles the read-only site commands:
// `srv list`, `srv info`, and `srv logs`.
package cmd

import (
	"fmt"
	"sort"
	"sync"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

// =============================================================================
// list command
// =============================================================================

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all sites",
	RunE:    runList,
}

func init() {
	listCmd.GroupID = GroupSites
	RootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	sites, err := site.List()
	if err != nil {
		return err
	}

	if len(sites) == 0 {
		ui.Dim("No sites registered. Use 'srv add PATH' to add a site.")
		return nil
	}

	// Sort by name
	sort.Slice(sites, func(i, j int) bool {
		return sites[i].Name < sites[j].Name
	})

	// Build table
	headers := []string{"NAME", "DOMAIN", "TARGET", "TYPE", "SSL", "STATUS"}
	rows := make([][]string, 0, len(sites))

	for _, s := range sites {
		status := s.Status
		if s.IsBroken {
			status = constants.StatusBroken
		}

		// Determine SSL status
		sslStatus := getSSLStatus(s)

		// Show directory path as target (or placeholder if broken)
		target := s.Dir
		if s.IsBroken {
			target = ui.DimText("-")
		}

		rows = append(rows, []string{
			s.Name,
			formatDomainsForList(s.Domains),
			target,
			getSiteTypeLabel(s),
			sslStatus,
			ui.StatusColor(status),
		})
	}

	ui.PrintTable(headers, rows)
	return nil
}

// formatDomainsForList renders a site's domains for the `srv list` table.
// Returns the primary alone if only one is set; otherwise primary plus a
// "+N" indicator so the table stays narrow.
func formatDomainsForList(domains []string) string {
	switch len(domains) {
	case 0:
		return ""
	case 1:
		return domains[0]
	default:
		return fmt.Sprintf("%s (+%d)", domains[0], len(domains)-1)
	}
}

// getSiteTypeLabel returns the site type label for the list view.
func getSiteTypeLabel(s site.Site) string {
	if s.IsBroken {
		return ui.DimText("-")
	}
	switch s.Type {
	case site.SiteTypeStatic:
		return "static"
	case site.SiteTypePHP:
		return "php"
	case site.SiteTypeNode:
		return "node"
	case site.SiteTypeRuby:
		return "ruby"
	case site.SiteTypePython:
		return "python"
	case site.SiteTypeDockerfile:
		return "dockerfile"
	default:
		return "compose"
	}
}

// getSSLStatus returns a formatted SSL status string for a site
func getSSLStatus(s site.Site) string {
	if s.IsBroken {
		return ui.DimText("-")
	}

	if s.IsLocal {
		// Local site - check mkcert certificate (named after the primary domain)
		cert := traefik.GetLocalCertInfo(s.Name, s.Domain())
		if cert.Corrupt {
			return ui.StatusColor("corrupt")
		}
		if !cert.Exists {
			return ui.StatusColor("missing")
		}
		if cert.IsExpired {
			return ui.StatusColor("expired")
		}
		if cert.DaysLeft <= constants.CertExpiryWarningDays {
			return ui.StatusColor("expiring")
		}
		return ui.StatusColor("valid")
	}

	// Production site - Let's Encrypt (auto-managed)
	return ui.DimText("auto")
}

// =============================================================================
// info command
// =============================================================================

var infoCmd = &cobra.Command{
	Use:   "info SITE",
	Short: "Show site info",
	Long: `Display detailed information about a site including:
  - Site name and path
  - Domain and type (local/production)
  - Container status
  - SSL certificate status (for local sites)`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			_ = cmd.Help()
			return ui.UsageError("srv info SITE", "a site name is required")
		}
		if len(args) > 1 {
			return ui.UsageError("srv info SITE", "too many arguments — expected a single site name, got %d", len(args))
		}
		return nil
	},
	RunE: runInfo,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	infoCmd.GroupID = GroupSites
	RootCmd.AddCommand(infoCmd)
}

func runInfo(cmd *cobra.Command, args []string) error {
	s, err := site.GetByName(args[0])
	if err != nil {
		return err
	}

	ui.Blank()
	ui.Bold("Site: %s", s.Name)
	ui.Blank()

	// Basic info
	ui.Print("  Path:    %s", s.Dir)
	if len(s.Domains) > 0 {
		ui.Print("  Domain:  %s", s.Domains[0])
		for _, alias := range s.Domains[1:] {
			ui.Print("  Alias:   %s", alias)
		}
	}
	ui.Print("  SSL:     %s", ui.TypeColor(s.IsLocal))

	// Site type info
	meta, _ := site.ReadSiteMetadata(s.Name)
	switch s.Type {
	case site.SiteTypeStatic:
		ui.Print("  Type:    %s", "static (nginx)")
	case site.SiteTypePHP:
		typeLabel := "php (nginx + php-fpm)"
		if meta != nil && meta.PHPVersion != "" && meta.PHPVersion != "latest" {
			typeLabel = fmt.Sprintf("php %s (nginx + php-fpm)", meta.PHPVersion)
		}
		ui.Print("  Type:    %s", typeLabel)
		if meta != nil && meta.PHPFramework != "" && meta.PHPFramework != "generic" {
			ui.Print("  Framework: %s", meta.PHPFramework)
		}
		if meta != nil && len(meta.PHPExtensions) > 0 {
			ui.Print("  Extensions: %d loaded", len(meta.PHPExtensions))
		}
	case site.SiteTypeNode:
		runtimeLabel := "node.js"
		if meta != nil && meta.NodeRuntime != "" {
			runtimeLabel = meta.NodeRuntime
			if meta.NodePackageManager != "" && meta.NodePackageManager != meta.NodeRuntime && meta.NodePackageManager != constants.NodePMDeno {
				runtimeLabel += " / " + meta.NodePackageManager
			}
			if meta.NodeVersion != "" && meta.NodeVersion != constants.NodeVersionLTS {
				runtimeLabel += " " + meta.NodeVersion
			}
		}
		ui.Print("  Type:    %s", runtimeLabel)
		if meta != nil && meta.NodeFramework != "" && meta.NodeFramework != "generic" {
			ui.Print("  Framework: %s", meta.NodeFramework)
		}
		if s.Port != 0 {
			ui.Print("  Port:    %d", s.Port)
		}
	case site.SiteTypeRuby:
		runtimeLabel := "ruby"
		if meta != nil && meta.RubyVersion != "" && meta.RubyVersion != constants.RubyVersionLatest {
			runtimeLabel = "ruby " + meta.RubyVersion
		}
		ui.Print("  Type:    %s", runtimeLabel)
		if meta != nil && meta.RubyFramework != "" && meta.RubyFramework != "generic" {
			ui.Print("  Framework: %s", meta.RubyFramework)
		}
		if s.Port != 0 {
			ui.Print("  Port:    %d", s.Port)
		}
	case site.SiteTypePython:
		runtimeLabel := "python"
		if meta != nil && meta.PythonVersion != "" && meta.PythonVersion != constants.PythonVersionLatest {
			runtimeLabel = "python " + meta.PythonVersion
		}
		ui.Print("  Type:    %s", runtimeLabel)
		if meta != nil && meta.PythonFramework != "" && meta.PythonFramework != "generic" {
			ui.Print("  Framework: %s", meta.PythonFramework)
		}
		if s.Port != 0 {
			ui.Print("  Port:    %d", s.Port)
		}
	case site.SiteTypeDockerfile:
		ui.Print("  Type:    %s", "dockerfile (custom build)")
		if s.Port != 0 {
			ui.Print("  Port:    %d", s.Port)
		}
	default:
		ui.Print("  Type:    %s", "compose")
		if s.ServiceName != "" {
			ui.Print("  Service: %s", s.ServiceName)
		}
		if s.Port != 0 {
			ui.Print("  Port:    %d", s.Port)
		}
	}

	cfg, _ := config.Load()
	if cfg != nil {
		ui.Print("  Config:  %s/sites/%s/", cfg.Root, s.Name)
	}

	// Status
	if s.IsBroken {
		ui.Print("  Status:  %s", ui.StatusColor("broken"))
		ui.IndentedWarn(1, "Target directory is missing")
	} else {
		ui.Print("  Status:  %s", ui.StatusColor(s.Status))
	}

	ui.Blank()

	// SSL certificate info for local sites
	if s.IsLocal && s.Domain() != "" {
		showCertInfo(s.Domain())
	}

	// Show URL if running
	if s.Status == constants.StatusRunning && s.Domain() != "" {
		ui.Blank()
		ui.Info("URL: https://%s", s.Domain())
	}

	ui.Blank()
	return nil
}

// showCertInfo displays SSL certificate information for a domain
func showCertInfo(domain string) {
	certs := traefik.ListLocalCerts()
	for _, cert := range certs {
		if cert.Domain == domain {
			ui.Bold("SSL Certificate")
			ui.Print("  Domain:  %s", cert.Domain)

			if cert.IsExpired {
				ui.Print("  Status:  %s", ui.StatusColor("expired"))
			} else if cert.DaysLeft <= constants.CertExpiryWarningDays {
				ui.Print("  Status:  %s (%d days left)", ui.StatusColor("expiring"), cert.DaysLeft)
			} else {
				ui.Print("  Status:  %s (%d days left)", ui.StatusColor("valid"), cert.DaysLeft)
			}

			ui.Print("  Expires: %s", cert.ExpiresAt.Format(constants.DateFormat))
			return
		}
	}

	ui.Bold("SSL Certificate")
	ui.Dim("  No certificate found for %s", domain)
	ui.IndentedDim(1, "Certificate will be generated on 'srv start'")
}

// =============================================================================
// logs command
// =============================================================================

var logsFlags struct {
	follow bool
	all    bool
	tail   string
	since  string
}

var logsCmd = &cobra.Command{
	Use:   "logs [SITE]",
	Short: "Show site logs",
	Args: func(cmd *cobra.Command, args []string) error {
		if logsFlags.all {
			return cobra.NoArgs(cmd, args)
		}
		if len(args) == 0 {
			_ = cmd.Help()
			return ui.UsageError("srv logs SITE", "a site name is required (or pass --all)")
		}
		if len(args) > 1 {
			return ui.UsageError("srv logs SITE", "too many arguments — expected a single site name, got %d", len(args))
		}
		return nil
	},
	RunE: runLogs,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFlags.follow, "follow", "f", false, "Follow log output")
	logsCmd.Flags().BoolVarP(&logsFlags.all, "all", "a", false, "Multiplex logs from every running site (colour-prefixed)")
	logsCmd.Flags().StringVar(&logsFlags.tail, "tail", "", "Number of lines to show from the end")
	logsCmd.Flags().StringVar(&logsFlags.since, "since", "", "Show logs since timestamp (e.g., 10m, 1h)")
	logsCmd.GroupID = GroupSites
	RootCmd.AddCommand(logsCmd)
}

func runLogs(cmd *cobra.Command, args []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	if logsFlags.all {
		return runLogsAll()
	}

	s, err := site.GetByName(args[0])
	if err != nil {
		return err
	}

	if s.IsBroken {
		return fmt.Errorf("site '%s' is broken (target directory missing)", s.Name)
	}

	// Build args
	composeArgs := []string{"logs"}
	if logsFlags.follow {
		composeArgs = append(composeArgs, "-f")
	}
	if logsFlags.tail != "" {
		composeArgs = append(composeArgs, "--tail", logsFlags.tail)
	}
	if logsFlags.since != "" {
		composeArgs = append(composeArgs, "--since", logsFlags.since)
	}

	return docker.Compose(s.ComposeDir, composeArgs...)
}

// runLogsAll multiplexes `docker compose logs` for every non-broken site,
// prefixing each output line with the site name. Stops when stdin closes
// (Ctrl-C) or when --follow is off and every per-site tail completes.
func runLogsAll() error {
	sites, err := site.List()
	if err != nil {
		return err
	}
	var running []site.Site
	for _, s := range sites {
		if !s.IsBroken {
			running = append(running, s)
		}
	}
	if len(running) == 0 {
		ui.Dim("No sites registered")
		return nil
	}

	var wg sync.WaitGroup
	for _, s := range running {
		s := s
		wg.Add(1)
		go func() {
			defer wg.Done()
			composeArgs := []string{"logs"}
			if logsFlags.follow {
				composeArgs = append(composeArgs, "-f")
			}
			if logsFlags.tail != "" {
				composeArgs = append(composeArgs, "--tail", logsFlags.tail)
			}
			if logsFlags.since != "" {
				composeArgs = append(composeArgs, "--since", logsFlags.since)
			}
			// Prefix every line with the site name. ComposePrefixed shells out
			// to `docker compose logs` and streams output through a writer that
			// stamps each line.
			if err := docker.ComposePrefixed(s.ComposeDir, s.Name, composeArgs...); err != nil {
				ui.Warn("[%s] log stream ended: %v", s.Name, err)
			}
		}()
	}
	wg.Wait()
	return nil
}
