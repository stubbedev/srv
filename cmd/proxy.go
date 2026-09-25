package cmd

import (
	"net/url"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/proxy"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

// =============================================================================
// proxy command
// =============================================================================

var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Manage proxy routes",
	Long: `Proxy local domains to services running outside of Docker.

This is useful for proxying to local development servers or other
applications running on localhost ports.

Proxies always use local SSL (mkcert) and register with local DNS.`,
}

var proxyAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a proxy",
	Long: `Create a proxy from a local domain to a localhost port or Docker container.

Examples:
  # Proxy to a localhost port
  srv proxy add --domain api.test --port 3000
  srv proxy add -d myapp.test -p 8080

  # Proxy to a Docker container (container_name:port)
  srv proxy add --domain api.test --container myapp:3000
  srv proxy add -d myapp.test -c postgres:5432`,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if proxyAddFlags.domain == "" {
			_ = cmd.Help()
			return ui.UsageError("srv proxy add --domain DOMAIN --port PORT", "--domain is required (e.g. --domain api.test)")
		}
		return nil
	},
	RunE: runProxyAdd,
}

var proxyRemoveCmd = &cobra.Command{
	Use:     "remove NAME",
	Aliases: []string{"rm"},
	Short:   "Remove a proxy",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			_ = cmd.Help()
			return ui.UsageError("srv proxy remove NAME", "a proxy name is required")
		}
		if len(args) > 1 {
			return ui.UsageError("srv proxy remove NAME", "too many arguments — expected a single proxy name, got %d", len(args))
		}
		return nil
	},
	RunE: runProxyRemove,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return getProxyNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

var proxyListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all proxies",
	RunE:    runProxyList,
}

var proxyAddFlags struct {
	domain          string
	port            string
	container       string
	name            string
	force           bool
	wildcard        bool
	fallbackURL     string
	fallbackTimeout string
}

func init() {
	proxyCmd.AddCommand(proxyAddCmd)
	proxyCmd.AddCommand(proxyRemoveCmd)
	proxyCmd.AddCommand(proxyListCmd)

	proxyAddCmd.Flags().StringVarP(&proxyAddFlags.domain, "domain", "d", "", "Domain name (e.g., api.test)")
	proxyAddCmd.Flags().StringVarP(&proxyAddFlags.port, "port", "p", "", "Localhost port to proxy to")
	proxyAddCmd.Flags().StringVarP(&proxyAddFlags.container, "container", "c", "", "Docker container to proxy to (container:port)")
	proxyAddCmd.Flags().StringVarP(&proxyAddFlags.name, "name", "n", "", "Proxy name (default: derived from domain)")
	proxyAddCmd.Flags().BoolVarP(&proxyAddFlags.force, "force", "f", false, "Overwrite existing proxy configuration")
	proxyAddCmd.Flags().BoolVar(&proxyAddFlags.wildcard, "wildcard", false, "Also match one-level subdomains (e.g. *.foo.test)")
	proxyAddCmd.Flags().StringVar(&proxyAddFlags.fallbackURL, "fallback", "", "URL to proxy to when the primary upstream returns 5xx (e.g. https://prod.example.com)")
	proxyAddCmd.Flags().StringVar(&proxyAddFlags.fallbackTimeout, "fallback-timeout", constants.FallbackTimeoutDefault, "Connect timeout to the primary upstream before falling back")
	_ = proxyAddCmd.MarkFlagRequired("domain")

	proxyCmd.GroupID = GroupProxy
	RootCmd.AddCommand(proxyCmd)
}

// =============================================================================
// Proxy Command Handlers
// =============================================================================

// runProxyAdd delegates entirely to internal/proxy.Add: validation, cert,
// DNS, container networking, the --fallback failover, config, and metadata are
// all shared with the MCP add_proxy tool, so the CLI is a thin flag mapper.
func runProxyAdd(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	res, err := proxy.Add(cfg, proxy.AddSpec{
		Name:            proxyAddFlags.name,
		Domain:          proxyAddFlags.domain,
		Port:            proxyAddFlags.port,
		Container:       proxyAddFlags.container,
		Wildcard:        proxyAddFlags.wildcard,
		Force:           proxyAddFlags.force,
		FallbackURL:     proxyAddFlags.fallbackURL,
		FallbackTimeout: proxyAddFlags.fallbackTimeout,
	})
	if err != nil {
		return err
	}
	for _, note := range res.Notes {
		ui.Dim("%s", note)
	}
	for _, w := range res.Warnings {
		ui.Warn("%s", w)
	}
	ui.Success("Proxy '%s' created", res.Name)
	ui.Dim("https://%s -> %s", res.Domain, res.TargetURL)
	return nil
}

func runProxyRemove(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// The shared removal (config, cert, DNS, routes, metadata, and the
	// --fallback failover when one exists) lives in internal/proxy so the CLI
	// and the MCP remove_proxy tool stay in lockstep.
	warnings, err := proxy.RemoveProxy(cfg, name)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		ui.Warn("%s", w)
	}

	ui.Success("Proxy '%s' removed", name)
	return nil
}

// proxyListRow is the json shape for one entry under `srv proxy list --format json`.
type proxyListRow struct {
	Name      string `json:"name"`
	Domain    string `json:"domain"`
	Target    string `json:"target"`
	Type      string `json:"type"`
	Container string `json:"container,omitempty"`
	SSL       string `json:"ssl"`
	Status    string `json:"status"`
}

func runProxyList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	proxies := getProxyNames()
	if len(proxies) == 0 {
		if jsonOutput() {
			return ui.PrintJSON([]proxyListRow{})
		}
		ui.Dim("No proxies configured. Use 'srv proxy add --domain DOMAIN --port PORT' to create one.")
		return nil
	}

	traefikUp := traefik.IsRunning()
	status := "inactive"
	if traefikUp {
		status = "active"
	}

	if jsonOutput() {
		out := make([]proxyListRow, 0, len(proxies))
		for _, name := range proxies {
			info := readProxyConfig(cfg, name)
			ptype := constants.ProxyTypeLocalhost
			if info.Container != "" {
				ptype = constants.ProxyTypeContainer
			}
			out = append(out, proxyListRow{
				Name:      name,
				Domain:    info.Domain,
				Target:    info.Target,
				Type:      ptype,
				Container: info.Container,
				SSL:       plainProxySSLStatus(name, info.Domain),
				Status:    status,
			})
		}
		return ui.PrintJSON(out)
	}

	headers := []string{"NAME", "DOMAIN", "TARGET", "TYPE", "SSL", "STATUS"}
	rows := make([][]string, 0, len(proxies))
	for _, name := range proxies {
		info := readProxyConfig(cfg, name)
		sslStatus := getProxySSLStatus(name, info.Domain)
		ptype := constants.ProxyTypeLocalhost
		if info.Container != "" {
			ptype = constants.ProxyTypeContainer
		}
		rows = append(rows, []string{name, info.Domain, info.Target, ptype, sslStatus, ui.StatusColor(status)})
	}
	ui.PrintTable(headers, rows)
	return nil
}

// plainProxySSLStatus mirrors getProxySSLStatus without colour codes for json.
func plainProxySSLStatus(name, domain string) string {
	return localCertStatus(proxy.CertSiteName(name), domain)
}

// =============================================================================
// Proxy Helpers
// =============================================================================

// getProxySSLStatus returns a formatted SSL status string for a proxy.
func getProxySSLStatus(name, domain string) string {
	return localCertStatusColored(proxy.CertSiteName(name), domain)
}

func getProxyNames() []string {
	return scanConfigNames(constants.ProxyConfigPrefix)
}

// proxyConfigInfo holds information read from a proxy config file.
type proxyConfigInfo struct {
	Domain    string
	Target    string
	Container string
}

// extractContainerFromURL extracts the container name from a target URL.
// Returns empty string if the target resolves to the host machine (localhost,
// 127.0.0.1, ::1, or host.docker.internal) rather than a named container.
func extractContainerFromURL(targetURL string) string {
	parsed, err := url.Parse(targetURL)
	if err != nil {
		return ""
	}

	host := parsed.Hostname()
	switch host {
	case constants.DockerHostInternal, constants.LocalhostIP, "localhost", "::1":
		return ""
	}

	return host
}

// readProxyConfig reads and parses a proxy configuration file.
// Returns a proxyConfigInfo with all available fields populated.
func readProxyConfig(cfg *config.Config, name string) proxyConfigInfo {
	proxyFile := filepath.Join(cfg.TraefikConfDir(), constants.ProxyConfigPrefix+name+constants.ExtYAML)
	data, err := os.ReadFile(proxyFile)
	if err != nil {
		return proxyConfigInfo{Target: "unknown"}
	}

	info := proxyConfigInfo{Target: "unknown"}

	// Parse the YAML structure
	var config traefik.RouteConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return info
	}

	// Extract domain from first router's rule (use shared function from traefik package)
	for _, router := range config.HTTP.Routers {
		if domain := traefik.ExtractDomainFromRule(router.Rule); domain != "" {
			info.Domain = domain
			break
		}
	}

	// Extract target URL from first service's first server
	for _, service := range config.HTTP.Services {
		if len(service.LoadBalancer.Servers) > 0 {
			info.Target = service.LoadBalancer.Servers[0].URL
			break
		}
	}

	// Extract container name from target URL using proper URL parsing
	info.Container = extractContainerFromURL(info.Target)

	return info
}
