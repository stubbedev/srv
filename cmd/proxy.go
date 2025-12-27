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
	Short: "Manage proxied services",
	Long: `Proxy domains to services running outside of Docker.

This is useful for proxying to services running on other ports,
such as local development servers or other applications.`,
}

var proxyAddCmd = &cobra.Command{
	Use:   "add NAME URL",
	Short: "Add a proxy to a service",
	Long: `Create a proxy from a .test domain to a local service URL.

Example:
  srv proxy add api http://127.0.0.1:3000
  srv proxy add app http://localhost:8000 --secure`,
	Args: cobra.ExactArgs(2),
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
	Short:   "List all proxies",
	RunE:    runProxyList,
}

var proxyAddFlags struct {
	secure bool
}

func init() {
	proxyCmd.AddCommand(proxyAddCmd)
	proxyCmd.AddCommand(proxyRemoveCmd)
	proxyCmd.AddCommand(proxyListCmd)
	proxyAddCmd.Flags().BoolVarP(&proxyAddFlags.secure, "secure", "s", false, "Use HTTPS for the proxy")
	RootCmd.AddCommand(proxyCmd)
}

func runProxyAdd(cmd *cobra.Command, args []string) error {
	name := args[0]
	targetURL := args[1]
	domain := name + ".test"

	// Validate inputs
	if err := ValidateSiteName(name); err != nil {
		return fmt.Errorf("invalid proxy name: %w", err)
	}
	if err := ValidateProxyURL(targetURL); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Check mkcert if using secure
	if proxyAddFlags.secure {
		if err := traefik.CheckMkcert(); err != nil {
			return err
		}
		if !traefik.IsCAInstalled() {
			return fmt.Errorf("mkcert CA not installed. Run 'srv trust' first")
		}

		// Generate certificate
		if err := traefik.EnsureLocalCert(domain); err != nil {
			return fmt.Errorf("failed to generate certificate: %w", err)
		}
	}

	// Create proxy config file
	if err := writeProxyConfig(cfg, name, domain, targetURL, proxyAddFlags.secure); err != nil {
		return err
	}

	// Update Traefik dynamic config
	if err := traefik.UpdateDynamicConfig(); err != nil {
		ui.Warn("Warning: Failed to update Traefik config: %v", err)
	}

	ui.Success("Proxy '%s' created", name)
	protocol := "http"
	if proxyAddFlags.secure {
		protocol = "https"
	}
	ui.Dim("%s://%s -> %s", protocol, domain, targetURL)
	return nil
}

func runProxyRemove(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Remove proxy config file
	proxyFile := filepath.Join(cfg.TraefikConfDir(), "proxy-"+name+".yml")
	if err := os.Remove(proxyFile); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("proxy '%s' not found", name)
		}
		return fmt.Errorf("failed to remove proxy: %w", err)
	}

	// Remove certificate if exists (best effort cleanup)
	domain := name + ".test"
	if err := traefik.RemoveLocalCerts(domain); err != nil {
		ui.Warn("Warning: Failed to remove certificate: %v", err)
	}
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
		ui.Dim("No proxies configured. Use 'srv proxy add NAME URL' to create one.")
		return nil
	}

	headers := []string{"NAME", "DOMAIN", "TARGET"}
	rows := make([][]string, 0, len(proxies))

	for _, name := range proxies {
		domain := name + ".test"
		target := readProxyTarget(cfg, name)
		rows = append(rows, []string{name, domain, target})
	}

	ui.PrintTable(headers, rows)
	return nil
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

func writeProxyConfig(cfg *config.Config, name, domain, targetURL string, secure bool) error {
	entrypoint := "web"
	if secure {
		entrypoint = "websecure"
	}

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
		EntryPoints: []string{entrypoint},
		Service:     "proxy-" + name,
	}
	if secure {
		router.TLS = &struct{}{}
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
	content := fmt.Sprintf("# Proxy configuration for %s - generated by srv\n%s", name, data)

	proxyFile := filepath.Join(cfg.TraefikConfDir(), "proxy-"+name+".yml")
	return os.WriteFile(proxyFile, []byte(content), 0o644)
}

func readProxyTarget(cfg *config.Config, name string) string {
	proxyFile := filepath.Join(cfg.TraefikConfDir(), "proxy-"+name+".yml")
	data, err := os.ReadFile(proxyFile)
	if err != nil {
		return "unknown"
	}

	// Simple extraction of URL from the config
	content := string(data)
	if idx := strings.Index(content, "url: \""); idx != -1 {
		start := idx + 6
		end := strings.Index(content[start:], "\"")
		if end != -1 {
			return content[start : start+end]
		}
	}
	return "unknown"
}
