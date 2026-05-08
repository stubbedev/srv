package cmd

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
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
	domain    string
	port      string
	container string
	name      string
	force     bool
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
	_ = proxyAddCmd.MarkFlagRequired("domain")

	proxyCmd.GroupID = GroupProxy
	RootCmd.AddCommand(proxyCmd)
}

// =============================================================================
// Proxy Input Validation
// =============================================================================

// proxyInput holds validated input for creating a proxy.
type proxyInput struct {
	name          string
	domain        string
	port          string
	containerName string
	containerPort string
	isContainer   bool
}

// validateProxyInput validates and parses proxy add command inputs.
func validateProxyInput() (*proxyInput, error) {
	domain := proxyAddFlags.domain
	port := proxyAddFlags.port
	container := proxyAddFlags.container

	// Validate that either port or container is provided, but not both
	if port == "" && container == "" {
		return nil, fmt.Errorf("either --port or --container must be specified")
	}
	if port != "" && container != "" {
		return nil, fmt.Errorf("--port and --container are mutually exclusive")
	}

	// Validate domain
	if err := ValidateDomain(domain); err != nil {
		return nil, fmt.Errorf("invalid domain: %w", err)
	}

	input := &proxyInput{
		domain: domain,
	}

	// Parse container flag (format: container_name:port)
	if container != "" {
		parts := strings.SplitN(container, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid container format. Use: container_name:port (e.g., myapp:3000)")
		}
		input.containerName = parts[0]
		input.containerPort = parts[1]
		input.isContainer = true

		if err := ValidatePortString(input.containerPort); err != nil {
			return nil, fmt.Errorf("invalid container port: %w", err)
		}

		// Check if container exists
		if !docker.ContainerExists(input.containerName) {
			return nil, fmt.Errorf("container '%s' does not exist", input.containerName)
		}
	} else {
		// Validate localhost port
		if err := ValidatePortString(port); err != nil {
			return nil, fmt.Errorf("invalid port: %w", err)
		}
		input.port = port
	}

	// Derive name from domain if not provided
	name := proxyAddFlags.name
	if name == "" {
		// Use SanitizeName for consistency with site add (dots become dashes)
		name = site.SanitizeName(domain)
	}

	if err := ValidateProxyName(name); err != nil {
		return nil, fmt.Errorf("invalid proxy name: %w", err)
	}
	input.name = name

	return input, nil
}

// =============================================================================
// Proxy Certificate Setup
// =============================================================================

// setupProxyCertificate ensures mkcert is installed and generates/renews the certificate.
func setupProxyCertificate(input *proxyInput) error {
	// Setup mkcert
	if err := traefik.CheckMkcert(); err != nil {
		return err
	}

	// Auto-install CA if not already installed
	if !traefik.IsCAInstalled() {
		ui.Dim("Installing mkcert CA...")
		res, err := traefik.InstallCA()
		if err != nil {
			return fmt.Errorf("failed to install mkcert CA: %w", err)
		}
		reportCAInstall(res, false)
	}

	// Generate certificate (or renew if expiring)
	// Use "_proxy-{name}" as the site name for cert storage
	proxySiteName := "_proxy-" + input.name
	renewed, err := traefik.EnsureLocalCert(proxySiteName, input.domain)
	if err != nil {
		return fmt.Errorf("failed to generate certificate: %w", err)
	}
	if renewed {
		ui.Dim("Generated SSL certificate for %s", input.domain)
	}

	return nil
}

// =============================================================================
// Proxy Container Network Setup
// =============================================================================

// connectProxyContainer connects a container to the srv network.
// Returns the target URL for the proxy.
func connectProxyContainer(input *proxyInput, cfg *config.Config) (string, error) {
	if !input.isContainer {
		// Warn if nothing is listening on the port yet so the proxy isn't silently broken.
		// Not a hard error: users often register a proxy before starting their dev server.
		conn, dialErr := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", input.port), 500*time.Millisecond)
		if dialErr != nil {
			ui.Warn("Nothing is listening on port %s — start your service before using the proxy", input.port)
		} else {
			_ = conn.Close()
		}

		// On Linux, Traefik uses network_mode: host, so it can reach localhost directly.
		// Use "localhost" rather than "127.0.0.1" so that services bound only to the
		// IPv6 loopback (::1) — e.g. Nuxt, Vite — are also reachable.
		// On Mac/Windows, Traefik runs in bridge mode and needs host.docker.internal.
		host := constants.DockerHostInternal
		if runtime.GOOS == "linux" {
			host = constants.LocalhostAlias
		}
		return fmt.Sprintf("http://%s:%s", host, input.port), nil
	}

	// Connect container to Traefik network so it can be reached.
	// CreateNetwork is idempotent — treats "already exists" as success.
	if err := docker.CreateNetwork(cfg.NetworkName); err != nil {
		return "", fmt.Errorf("failed to create network: %w", err)
	}

	if err := docker.ConnectContainerToNetwork(input.containerName, cfg.NetworkName, ""); err != nil {
		return "", fmt.Errorf("failed to connect container to network: %w", err)
	}
	ui.Dim("Connected container '%s' to %s network", input.containerName, cfg.NetworkName)

	return fmt.Sprintf("http://%s:%s", input.containerName, input.containerPort), nil
}

// =============================================================================
// Proxy Command Handlers
// =============================================================================

func runProxyAdd(cmd *cobra.Command, args []string) error {
	// Validate input
	input, err := validateProxyInput()
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Check if proxy already exists
	proxyFile := filepath.Join(cfg.TraefikConfDir(), constants.ProxyConfigPrefix+input.name+constants.ExtYAML)
	if _, err := os.Stat(proxyFile); err == nil && !proxyAddFlags.force {
		return fmt.Errorf("proxy '%s' already exists. Use --force to overwrite", input.name)
	}

	// Setup certificate
	if err := setupProxyCertificate(input); err != nil {
		return err
	}

	// Register domain for local DNS
	if err := traefik.RegisterLocalDomain(input.domain); err != nil {
		ui.Warn("Failed to register DNS for %s: %v", input.domain, err)
	}

	// Connect container if needed and get target URL
	targetURL, err := connectProxyContainer(input, cfg)
	if err != nil {
		return err
	}

	// Create proxy config file
	if err := writeProxyConfig(cfg, input.name, input.domain, targetURL, input.containerName); err != nil {
		return err
	}

	// Update Traefik dynamic config
	if err := traefik.UpdateDynamicConfig(); err != nil {
		ui.Warn("Failed to update Traefik config: %v", err)
	}

	ui.Success("Proxy '%s' created", input.name)
	if input.isContainer {
		ui.Dim("https://%s -> %s:%s (container)", input.domain, input.containerName, input.containerPort)
	} else {
		ui.Dim("https://%s -> localhost:%s", input.domain, input.port)
		ui.Dim("Start your service on port %s to use this proxy", input.port)
	}
	return nil
}

func runProxyRemove(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Read proxy config to get domain before removing
	proxyInfo := readProxyConfig(cfg, name)

	// Remove proxy config file
	proxyFile := filepath.Join(cfg.TraefikConfDir(), constants.ProxyConfigPrefix+name+constants.ExtYAML)
	if err := os.Remove(proxyFile); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("proxy '%s' not found", name)
		}
		return fmt.Errorf("failed to remove proxy: %w", err)
	}

	// Remove certificate and DNS if we found the domain
	if proxyInfo.Domain != "" {
		// Use "_proxy-{name}" as the site name for cert storage
		proxySiteName := "_proxy-" + name
		if err := traefik.RemoveLocalCerts(proxySiteName, proxyInfo.Domain); err != nil {
			ui.Warn("Failed to remove certificate: %v", err)
		}
		if err := traefik.UnregisterLocalDomain(proxyInfo.Domain); err != nil {
			ui.Warn("Failed to unregister DNS for %s: %v", proxyInfo.Domain, err)
		}
	}

	// Update Traefik dynamic config
	if err := traefik.UpdateDynamicConfig(); err != nil {
		ui.Warn("Failed to update Traefik config: %v", err)
	}

	ui.Success("Proxy '%s' removed", name)
	return nil
}

func runProxyList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	proxies := getProxyNames()
	if len(proxies) == 0 {
		ui.Dim("No proxies configured. Use 'srv proxy add --domain DOMAIN --port PORT' to create one.")
		return nil
	}

	traefikUp := traefik.IsRunning()

	headers := []string{"NAME", "DOMAIN", "TARGET", "TYPE", "SSL", "STATUS"}
	rows := make([][]string, 0, len(proxies))

	for _, name := range proxies {
		proxyInfo := readProxyConfig(cfg, name)
		sslStatus := getProxySSLStatus(name, proxyInfo.Domain)

		// Determine type based on whether it's proxying to a container or localhost
		proxyType := constants.ProxyTypeLocalhost
		if proxyInfo.Container != "" {
			proxyType = constants.ProxyTypeContainer
		}

		// Proxy status depends on Traefik being up; proxies have no containers of their own
		status := "inactive"
		if traefikUp {
			status = "active"
		}

		rows = append(rows, []string{name, proxyInfo.Domain, proxyInfo.Target, proxyType, sslStatus, ui.StatusColor(status)})
	}

	ui.PrintTable(headers, rows)
	return nil
}

// =============================================================================
// Proxy Helpers
// =============================================================================

// getProxySSLStatus returns a formatted SSL status string for a proxy.
func getProxySSLStatus(name, domain string) string {
	if domain == "" {
		return ui.DimText("-")
	}

	// All proxies use local mkcert certificates
	// Use "_proxy-{name}" as the site name for cert storage
	proxySiteName := "_proxy-" + name
	cert := traefik.GetLocalCertInfo(proxySiteName, domain)
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

func getProxyNames() []string {
	cfg, err := config.Load()
	if err != nil {
		ui.VerboseLog("Warning: could not load config: %v", err)
		return nil
	}

	entries, err := os.ReadDir(cfg.TraefikConfDir())
	if err != nil {
		ui.VerboseLog("Warning: could not read traefik conf dir: %v", err)
		return nil
	}

	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, constants.ProxyConfigPrefix) && strings.HasSuffix(name, constants.ExtYAML) {
			proxyName := strings.TrimSuffix(strings.TrimPrefix(name, constants.ProxyConfigPrefix), constants.ExtYAML)
			names = append(names, proxyName)
		}
	}
	return names
}

// =============================================================================
// Proxy Config File Operations
// =============================================================================

func writeProxyConfig(cfg *config.Config, name, domain, targetURL, containerName string) error {
	// Build the config using proper types to avoid YAML injection
	type Server struct {
		URL string `yaml:"url"`
	}
	type LoadBalancer struct {
		Servers []Server `yaml:"servers"`
	}
	type Service struct {
		LoadBalancer LoadBalancer `yaml:"loadBalancer"`
	}
	type Router struct {
		Rule        string    `yaml:"rule"`
		EntryPoints []string  `yaml:"entryPoints"`
		Service     string    `yaml:"service"`
		TLS         *struct{} `yaml:"tls,omitempty"`
	}
	type HTTP struct {
		Routers  map[string]Router  `yaml:"routers"`
		Services map[string]Service `yaml:"services"`
	}
	type ProxyConfig struct {
		HTTP HTTP `yaml:"http"`
	}

	router := Router{
		Rule:        fmt.Sprintf("Host(`%s`)", domain),
		EntryPoints: []string{constants.EntryPointWebsecure},
		Service:     constants.ProxyConfigPrefix + name,
		TLS:         &struct{}{},
	}

	proxyConfig := ProxyConfig{
		HTTP: HTTP{
			Routers: map[string]Router{
				constants.ProxyConfigPrefix + name: router,
			},
			Services: map[string]Service{
				constants.ProxyConfigPrefix + name: {
					LoadBalancer: LoadBalancer{
						Servers: []Server{{URL: targetURL}},
					},
				},
			},
		},
	}

	data, err := yaml.Marshal(&proxyConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal proxy config: %w", err)
	}

	// Add header comment (include container name if proxying to a container)
	var content string
	if containerName != "" {
		content = fmt.Sprintf("# Proxy configuration for %s - generated by srv\n# Domain: %s\n# Container: %s\n%s", name, domain, containerName, data)
	} else {
		content = fmt.Sprintf("# Proxy configuration for %s - generated by srv\n# Domain: %s\n%s", name, domain, data)
	}

	proxyFile := filepath.Join(cfg.TraefikConfDir(), constants.ProxyConfigPrefix+name+constants.ExtYAML)
	return os.WriteFile(proxyFile, []byte(content), constants.FilePermDefault)
}

// proxyConfigInfo holds information read from a proxy config file.
type proxyConfigInfo struct {
	Domain    string
	Target    string
	Container string
}

// traefikRouteConfig is an alias for the shared type in the traefik package.
// It exists here for backward compatibility within this file.
type traefikRouteConfig = traefik.RouteConfig

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
	var config traefikRouteConfig
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
