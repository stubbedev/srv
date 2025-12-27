package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

// =============================================================================
// proxy command
// =============================================================================

var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Manage proxies",
	Long: `Proxy local domains to services running outside of Docker.

This is useful for proxying to local development servers or other
applications running on localhost ports.

Proxies always use local SSL (mkcert) and register with local DNS.`,
}

var proxyAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a proxy",
	Long: `Create a proxy from a local domain to a localhost port.

Examples:
  srv proxy add --domain api.test --port 3000
  srv proxy add -d myapp.test -p 8080`,
	RunE: runProxyAdd,
}

var proxyRemoveCmd = &cobra.Command{
	Use:     "remove NAME",
	Aliases: []string{"rm"},
	Short:   "Remove a proxy",
	Args:    cobra.ExactArgs(1),
	RunE:    runProxyRemove,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return getProxyNames(), cobra.ShellCompDirectiveNoFileComp
	},
}

var proxyListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List proxies",
	RunE:    runProxyList,
}

var proxyAddFlags struct {
	domain string
	port   string
	name   string
}

func init() {
	proxyCmd.AddCommand(proxyAddCmd)
	proxyCmd.AddCommand(proxyRemoveCmd)
	proxyCmd.AddCommand(proxyListCmd)

	proxyAddCmd.Flags().StringVarP(&proxyAddFlags.domain, "domain", "d", "", "Domain name (e.g., api.test)")
	proxyAddCmd.Flags().StringVarP(&proxyAddFlags.port, "port", "p", "", "Localhost port to proxy to")
	proxyAddCmd.Flags().StringVarP(&proxyAddFlags.name, "name", "n", "", "Proxy name (default: derived from domain)")
	proxyAddCmd.MarkFlagRequired("domain")
	proxyAddCmd.MarkFlagRequired("port")

	RootCmd.AddCommand(proxyCmd)
}

func runProxyAdd(cmd *cobra.Command, args []string) error {
	domain := proxyAddFlags.domain
	port := proxyAddFlags.port

	// Validate domain
	if err := ValidateDomain(domain); err != nil {
		return fmt.Errorf("invalid domain: %w", err)
	}

	// Validate port
	if err := ValidatePort(port); err != nil {
		return fmt.Errorf("invalid port: %w", err)
	}

	// Derive name from domain if not provided
	name := proxyAddFlags.name
	if name == "" {
		// Use first part of domain as name (e.g., "api" from "api.test")
		name = strings.Split(domain, ".")[0]
	}

	if err := ValidateSiteName(name); err != nil {
		return fmt.Errorf("invalid proxy name: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Check if proxy already exists
	proxyFile := filepath.Join(cfg.TraefikConfDir(), "proxy-"+name+".yml")
	if _, err := os.Stat(proxyFile); err == nil {
		return fmt.Errorf("proxy '%s' already exists. Use 'srv proxy remove %s' first", name, name)
	}

	// Setup mkcert
	if err := traefik.CheckMkcert(); err != nil {
		return err
	}

	// Auto-install CA if not already installed
	if !traefik.IsCAInstalled() {
		ui.Dim("Installing mkcert CA...")
		if err := traefik.InstallCA(); err != nil {
			return fmt.Errorf("failed to install mkcert CA: %w", err)
		}
		ui.Success("mkcert CA installed")
		ui.Dim("Restart your browser for the CA to take effect")
	}

	// Generate certificate (or renew if expiring)
	renewed, err := traefik.EnsureLocalCert(domain)
	if err != nil {
		return fmt.Errorf("failed to generate certificate: %w", err)
	}
	if renewed {
		ui.Dim("Generated SSL certificate for %s", domain)
	}

	// Register domain for local DNS
	if err := traefik.RegisterLocalDomain(domain); err != nil {
		ui.Warn("Warning: Failed to register DNS for %s: %v", domain, err)
	}

	// Create proxy config file
	targetURL := fmt.Sprintf("http://host.docker.internal:%s", port)
	if err := writeProxyConfig(cfg, name, domain, targetURL); err != nil {
		return err
	}

	// Update Traefik dynamic config
	if err := traefik.UpdateDynamicConfig(); err != nil {
		ui.Warn("Warning: Failed to update Traefik config: %v", err)
	}

	ui.Success("Proxy '%s' created", name)
	ui.Dim("https://%s -> localhost:%s", domain, port)
	return nil
}

func runProxyRemove(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Read proxy config to get domain before removing
	domain := readProxyDomain(cfg, name)

	// Remove proxy config file
	proxyFile := filepath.Join(cfg.TraefikConfDir(), "proxy-"+name+".yml")
	if err := os.Remove(proxyFile); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("proxy '%s' not found", name)
		}
		return fmt.Errorf("failed to remove proxy: %w", err)
	}

	// Remove certificate and DNS if we found the domain
	if domain != "" {
		if err := traefik.RemoveLocalCerts(domain); err != nil {
			ui.Warn("Warning: Failed to remove certificate: %v", err)
		}
		if err := traefik.UnregisterLocalDomain(domain); err != nil {
			ui.Warn("Warning: Failed to unregister DNS for %s: %v", domain, err)
		}
	}

	// Update Traefik dynamic config
	if err := traefik.UpdateDynamicConfig(); err != nil {
		ui.Warn("Warning: Failed to update Traefik config: %v", err)
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

	headers := []string{"NAME", "DOMAIN", "TARGET", "SSL"}
	rows := make([][]string, 0, len(proxies))

	for _, name := range proxies {
		domain := readProxyDomain(cfg, name)
		target := readProxyTarget(cfg, name)
		sslStatus := getProxySSLStatus(domain)
		rows = append(rows, []string{name, domain, target, sslStatus})
	}

	ui.PrintTable(headers, rows)
	return nil
}

// getProxySSLStatus returns a formatted SSL status string for a proxy
func getProxySSLStatus(domain string) string {
	if domain == "" {
		return ui.DimText("-")
	}

	// All proxies use local mkcert certificates
	cert := traefik.GetLocalCertInfo(domain)
	if !cert.Exists {
		return ui.StatusColor("missing")
	}
	if cert.IsExpired {
		return ui.StatusColor("expired")
	}
	if cert.DaysLeft <= 30 {
		return ui.StatusColor("expiring")
	}
	return ui.StatusColor("valid")
}

func getProxyNames() []string {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}

	entries, err := os.ReadDir(cfg.TraefikConfDir())
	if err != nil {
		return nil
	}

	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "proxy-") && strings.HasSuffix(name, ".yml") {
			proxyName := strings.TrimSuffix(strings.TrimPrefix(name, "proxy-"), ".yml")
			names = append(names, proxyName)
		}
	}
	return names
}

func writeProxyConfig(cfg *config.Config, name, domain, targetURL string) error {
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
		Rule        string   `yaml:"rule"`
		EntryPoints []string `yaml:"entryPoints"`
		Service     string   `yaml:"service"`
		TLS         *struct {
		} `yaml:"tls,omitempty"`
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
		EntryPoints: []string{"websecure"},
		Service:     "proxy-" + name,
		TLS:         &struct{}{},
	}

	proxyConfig := ProxyConfig{
		HTTP: HTTP{
			Routers: map[string]Router{
				"proxy-" + name: router,
			},
			Services: map[string]Service{
				"proxy-" + name: {
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

	// Add header comment
	content := fmt.Sprintf("# Proxy configuration for %s - generated by srv\n# Domain: %s\n%s", name, domain, data)

	proxyFile := filepath.Join(cfg.TraefikConfDir(), "proxy-"+name+".yml")
	return os.WriteFile(proxyFile, []byte(content), 0o644)
}

func readProxyDomain(cfg *config.Config, name string) string {
	proxyFile := filepath.Join(cfg.TraefikConfDir(), "proxy-"+name+".yml")
	data, err := os.ReadFile(proxyFile)
	if err != nil {
		return ""
	}

	// Extract domain from comment or Host rule
	content := string(data)

	// Try comment first (# Domain: xxx)
	if idx := strings.Index(content, "# Domain: "); idx != -1 {
		start := idx + 10
		end := strings.Index(content[start:], "\n")
		if end != -1 {
			return strings.TrimSpace(content[start : start+end])
		}
	}

	// Fallback to Host rule
	if idx := strings.Index(content, "Host(`"); idx != -1 {
		start := idx + 6
		end := strings.Index(content[start:], "`)")
		if end != -1 {
			return content[start : start+end]
		}
	}

	return ""
}

func readProxyTarget(cfg *config.Config, name string) string {
	proxyFile := filepath.Join(cfg.TraefikConfDir(), "proxy-"+name+".yml")
	data, err := os.ReadFile(proxyFile)
	if err != nil {
		return "unknown"
	}

	// Extract URL from config
	content := string(data)
	if idx := strings.Index(content, "url: "); idx != -1 {
		start := idx + 5
		// Handle both quoted and unquoted URLs
		if content[start] == '"' {
			start++
			end := strings.Index(content[start:], "\"")
			if end != -1 {
				return content[start : start+end]
			}
		} else {
			end := strings.IndexAny(content[start:], "\n\r")
			if end != -1 {
				return strings.TrimSpace(content[start : start+end])
			}
		}
	}
	return "unknown"
}
