package traefik

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/shell"
)

// LocalDomains are the TLDs used for local development.
var LocalDomains = []string{"test", "local", "localhost"}

// DNS config file paths.
const (
	systemdResolvedConfig = "/etc/systemd/resolved.conf.d/srv-local.conf"
	networkManagerConfig  = "/etc/NetworkManager/dnsmasq.d/srv-local.conf"
	macOSResolverDir      = "/etc/resolver"
)

// DNSResolverType represents the type of DNS resolver on the system.
type DNSResolverType int

const (
	ResolverUnknown DNSResolverType = iota
	ResolverSystemdResolved
	ResolverMacOS
	ResolverNetworkManager
)

// DetectResolver detects the DNS resolver type on the system.
func DetectResolver() DNSResolverType {
	// Check for systemd-resolved
	if _, err := os.Stat("/run/systemd/resolve/stub-resolv.conf"); err == nil {
		return ResolverSystemdResolved
	}

	// Check for macOS resolver directory capability
	if runtime.GOOS == "darwin" {
		return ResolverMacOS
	}

	// Check for NetworkManager
	if shell.Exists("nmcli") {
		return ResolverNetworkManager
	}

	return ResolverUnknown
}

// CheckDNS tests if local DNS resolution is working for .test domains.
func CheckDNS() bool {
	output, err := shell.Dig("+short", "@127.0.0.1", "test.test")
	if err != nil {
		return false
	}
	return output == "127.0.0.1"
}

// CheckSystemDNS tests if the system resolves .test domains correctly.
func CheckSystemDNS() bool {
	output, err := shell.Dig("+short", "test.test")
	if err != nil {
		return false
	}
	return output == "127.0.0.1"
}

// SetupDNS configures the system to use the local DNS server for .test domains.
// Returns an error if setup fails or requires manual intervention.
func SetupDNS() error {
	resolver := DetectResolver()

	switch resolver {
	case ResolverSystemdResolved:
		return setupSystemdResolved()
	case ResolverMacOS:
		return setupMacOSResolver()
	case ResolverNetworkManager:
		return setupNetworkManager()
	default:
		return fmt.Errorf("unsupported DNS configuration. Please manually configure your system to use 127.0.0.1 for .test, .local, and .localhost domains")
	}
}

// setupSystemdResolved configures systemd-resolved for local domains.
func setupSystemdResolved() error {
	configFile := systemdResolvedConfig
	configDir := filepath.Dir(configFile)

	// Check if already configured
	if _, err := os.Stat(configFile); err == nil {
		return nil // Already configured
	}

	content := `[Resolve]
DNS=127.0.0.1
Domains=~test ~local ~localhost
`

	if err := shell.SudoMkdir(configDir); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := shell.SudoWrite(configFile, content); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	if err := shell.SudoSystemctl("restart", "systemd-resolved"); err != nil {
		return fmt.Errorf("failed to restart systemd-resolved: %w", err)
	}

	return nil
}

// setupMacOSResolver configures macOS resolver for local domains.
func setupMacOSResolver() error {
	if err := shell.SudoMkdir(macOSResolverDir); err != nil {
		return fmt.Errorf("failed to create resolver directory: %w", err)
	}

	for _, domain := range LocalDomains {
		resolverFile := filepath.Join(macOSResolverDir, domain)

		// Check if already configured
		if data, err := os.ReadFile(resolverFile); err == nil {
			if strings.Contains(string(data), "127.0.0.1") {
				continue // Already configured
			}
		}

		if err := shell.SudoWrite(resolverFile, "nameserver 127.0.0.1\n"); err != nil {
			return fmt.Errorf("failed to write resolver file for .%s: %w", domain, err)
		}
	}

	return nil
}

// setupNetworkManager configures NetworkManager to use local DNS for .test domains.
func setupNetworkManager() error {
	configFile := networkManagerConfig
	configDir := filepath.Dir(configFile)

	// Check if already configured
	if _, err := os.Stat(configFile); err == nil {
		return nil // Already configured
	}

	content := `# srv local DNS configuration
server=/test/127.0.0.1
server=/local/127.0.0.1
server=/localhost/127.0.0.1
`

	if err := shell.SudoMkdir(configDir); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := shell.SudoWrite(configFile, content); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	if err := shell.SudoSystemctl("restart", "NetworkManager"); err != nil {
		return fmt.Errorf("failed to restart NetworkManager: %w", err)
	}

	return nil
}

// RemoveDNS removes the DNS configuration set up by SetupDNS.
func RemoveDNS() error {
	resolver := DetectResolver()

	switch resolver {
	case ResolverSystemdResolved:
		if _, err := os.Stat(systemdResolvedConfig); os.IsNotExist(err) {
			return nil // Already removed
		}
		if err := shell.SudoRemove(systemdResolvedConfig); err != nil {
			return fmt.Errorf("failed to remove config file: %w", err)
		}
		return shell.SudoSystemctl("restart", "systemd-resolved")

	case ResolverMacOS:
		var lastErr error
		for _, domain := range LocalDomains {
			resolverFile := filepath.Join(macOSResolverDir, domain)
			if err := shell.SudoRemove(resolverFile); err != nil {
				// Only track errors for files that exist
				if _, statErr := os.Stat(resolverFile); statErr == nil {
					lastErr = err
				}
			}
		}
		return lastErr

	case ResolverNetworkManager:
		if _, err := os.Stat(networkManagerConfig); os.IsNotExist(err) {
			return nil // Already removed
		}
		if err := shell.SudoRemove(networkManagerConfig); err != nil {
			return fmt.Errorf("failed to remove config file: %w", err)
		}
		return shell.SudoSystemctl("restart", "NetworkManager")

	default:
		return nil
	}
}

// GetResolverName returns a human-readable name for the resolver type.
func GetResolverName() string {
	switch DetectResolver() {
	case ResolverSystemdResolved:
		return "systemd-resolved"
	case ResolverMacOS:
		return "macOS resolver"
	case ResolverNetworkManager:
		return "NetworkManager"
	default:
		return "unknown"
	}
}

// =============================================================================
// Local Domain Registry
// =============================================================================

// localDomainsFile returns the path to the local domains registry file.
func localDomainsFile() (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg.TraefikDir, "local-domains.txt"), nil
}

// LoadLocalDomains returns the list of registered local domains.
func LoadLocalDomains() ([]string, error) {
	path, err := localDomainsFile()
	if err != nil {
		return nil, err
	}

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	defer file.Close()

	var domains []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		domain := strings.TrimSpace(scanner.Text())
		if domain != "" && !strings.HasPrefix(domain, "#") {
			domains = append(domains, domain)
		}
	}

	return domains, scanner.Err()
}

// SaveLocalDomains saves the list of local domains to the registry.
func SaveLocalDomains(domains []string) error {
	path, err := localDomainsFile()
	if err != nil {
		return err
	}

	// Sort and deduplicate
	sort.Strings(domains)
	unique := make([]string, 0, len(domains))
	seen := make(map[string]bool)
	for _, d := range domains {
		if !seen[d] {
			seen[d] = true
			unique = append(unique, d)
		}
	}

	content := strings.Join(unique, "\n")
	if len(unique) > 0 {
		content += "\n"
	}

	return os.WriteFile(path, []byte(content), 0o644)
}

// RegisterLocalDomain adds a domain to the local DNS registry and updates dnsmasq.
func RegisterLocalDomain(domain string) error {
	domains, err := LoadLocalDomains()
	if err != nil {
		return err
	}

	// Check if already registered
	for _, d := range domains {
		if d == domain {
			return nil // Already registered
		}
	}

	domains = append(domains, domain)
	if err := SaveLocalDomains(domains); err != nil {
		return err
	}

	return UpdateDnsmasqConfig()
}

// UnregisterLocalDomain removes a domain from the local DNS registry and updates dnsmasq.
func UnregisterLocalDomain(domain string) error {
	domains, err := LoadLocalDomains()
	if err != nil {
		return err
	}

	// Filter out the domain
	filtered := make([]string, 0, len(domains))
	found := false
	for _, d := range domains {
		if d == domain {
			found = true
		} else {
			filtered = append(filtered, d)
		}
	}

	if !found {
		return nil // Not registered
	}

	if err := SaveLocalDomains(filtered); err != nil {
		return err
	}

	return UpdateDnsmasqConfig()
}

// UpdateDnsmasqConfig regenerates dnsmasq.conf based on registered domains and reloads DNS.
func UpdateDnsmasqConfig() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	domains, err := LoadLocalDomains()
	if err != nil {
		return err
	}

	// Generate dnsmasq.conf
	var content strings.Builder
	content.WriteString("# Local domains managed by srv\n")
	content.WriteString("# Do not edit manually - changes will be overwritten\n\n")

	if len(domains) == 0 {
		content.WriteString("# No local domains registered\n")
	} else {
		for _, domain := range domains {
			content.WriteString(fmt.Sprintf("address=/%s/127.0.0.1\n", domain))
		}
	}

	content.WriteString("\n# Forward all other queries to upstream DNS\n")
	content.WriteString("server=8.8.8.8\n")
	content.WriteString("server=8.8.4.4\n")
	content.WriteString("\n# Don't read /etc/resolv.conf\n")
	content.WriteString("no-resolv\n")

	dnsmasqPath := filepath.Join(cfg.TraefikDir, "dnsmasq.conf")
	if err := os.WriteFile(dnsmasqPath, []byte(content.String()), 0o644); err != nil {
		return fmt.Errorf("failed to write dnsmasq.conf: %w", err)
	}

	// Reload DNS container if running
	if IsDNSRunning() {
		return ReloadDNS()
	}

	return nil
}

// ReloadDNS restarts the DNS container to pick up config changes.
func ReloadDNS() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	return docker.Compose(cfg.TraefikDir, "restart", "dns")
}
