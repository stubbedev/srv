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
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/shell"
)

// LocalDomains are the TLDs used for local development.
var LocalDomains = []string{"test", "local", "localhost"}

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
	if _, err := os.Stat(constants.SystemdResolvePath); err == nil {
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
	output, err := shell.Dig("+short", "@"+constants.LocalhostIP, constants.DNSTestDomain)
	if err != nil {
		return false
	}
	return output == constants.LocalhostIP
}

// CheckSystemDNS tests if the system resolves .test domains correctly.
func CheckSystemDNS() bool {
	output, err := shell.Dig("+short", constants.DNSTestDomain)
	if err != nil {
		return false
	}
	return output == constants.LocalhostIP
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
	configFile := constants.SystemdResolvedConfigPath
	configDir := filepath.Dir(configFile)

	// Check if already configured
	if _, err := os.Stat(configFile); err == nil {
		return nil // Already configured
	}

	content := fmt.Sprintf(`[Resolve]
DNS=%s
Domains=~test ~local ~localhost
`, constants.LocalhostIP)

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
	if err := shell.SudoMkdir(constants.MacOSResolverDir); err != nil {
		return fmt.Errorf("failed to create resolver directory: %w", err)
	}

	for _, domain := range LocalDomains {
		resolverFile := filepath.Join(constants.MacOSResolverDir, domain)

		// Check if already configured
		if data, err := os.ReadFile(resolverFile); err == nil {
			if strings.Contains(string(data), constants.LocalhostIP) {
				continue // Already configured
			}
		}

		if err := shell.SudoWrite(resolverFile, "nameserver "+constants.LocalhostIP+"\n"); err != nil {
			return fmt.Errorf("failed to write resolver file for .%s: %w", domain, err)
		}
	}

	return nil
}

// setupNetworkManager configures NetworkManager to use local DNS for .test domains.
func setupNetworkManager() error {
	configFile := constants.NetworkManagerConfigPath
	configDir := filepath.Dir(configFile)

	// Check if already configured
	if _, err := os.Stat(configFile); err == nil {
		return nil // Already configured
	}

	content := fmt.Sprintf(`# srv local DNS configuration
server=/test/%s
server=/local/%s
server=/localhost/%s
`, constants.LocalhostIP, constants.LocalhostIP, constants.LocalhostIP)

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
		if _, err := os.Stat(constants.SystemdResolvedConfigPath); os.IsNotExist(err) {
			return nil // Already removed
		}
		if err := shell.SudoRemove(constants.SystemdResolvedConfigPath); err != nil {
			return fmt.Errorf("failed to remove config file: %w", err)
		}
		return shell.SudoSystemctl("restart", "systemd-resolved")

	case ResolverMacOS:
		var lastErr error
		for _, domain := range LocalDomains {
			resolverFile := filepath.Join(constants.MacOSResolverDir, domain)
			if err := shell.SudoRemove(resolverFile); err != nil {
				// Only track errors for files that exist
				if _, statErr := os.Stat(resolverFile); statErr == nil {
					lastErr = err
				}
			}
		}
		return lastErr

	case ResolverNetworkManager:
		if _, err := os.Stat(constants.NetworkManagerConfigPath); os.IsNotExist(err) {
			return nil // Already removed
		}
		if err := shell.SudoRemove(constants.NetworkManagerConfigPath); err != nil {
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
	return filepath.Join(cfg.TraefikDir, constants.LocalDomainsFile), nil
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

	return os.WriteFile(path, []byte(content), constants.FilePermDefault)
}

// RegisterLocalDomain adds a domain to the local DNS registry and updates dnsmasq.
// Automatically configures system DNS when the first local domain is added.
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

	// Check if this is the first local domain being added
	isFirstDomain := len(domains) == 0

	domains = append(domains, domain)
	if err := SaveLocalDomains(domains); err != nil {
		return err
	}

	if err := UpdateDnsmasqConfig(); err != nil {
		return err
	}

	// Automatically set up system DNS when adding the first local domain
	if isFirstDomain && !CheckSystemDNS() {
		if err := SetupDNS(); err != nil {
			// Non-fatal: log warning but don't fail the operation
			return fmt.Errorf("DNS registered but system DNS setup failed: %w", err)
		}
	}

	return nil
}

// UnregisterLocalDomain removes a domain from the local DNS registry and updates dnsmasq.
// Automatically removes system DNS configuration when the last local domain is removed.
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

	if err := UpdateDnsmasqConfig(); err != nil {
		return err
	}

	// Automatically remove system DNS when removing the last local domain
	if len(filtered) == 0 {
		if err := RemoveDNS(); err != nil {
			// Non-fatal: log warning but don't fail the operation
			return fmt.Errorf("DNS unregistered but system DNS removal failed: %w", err)
		}
	}

	return nil
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
	content.WriteString(fmt.Sprintf("server=%s\n", constants.GoogleDNS1))
	content.WriteString(fmt.Sprintf("server=%s\n", constants.GoogleDNS2))
	content.WriteString("\n# Don't read /etc/resolv.conf\n")
	content.WriteString("no-resolv\n")

	dnsmasqPath := filepath.Join(cfg.TraefikDir, constants.DnsmasqConfFile)
	if err := os.WriteFile(dnsmasqPath, []byte(content.String()), constants.FilePermDefault); err != nil {
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
