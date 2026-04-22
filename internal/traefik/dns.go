package traefik

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

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

// CheckDNS tests if the local DNS server resolves the given domain to localhost.
// It queries 127.0.0.1:53 directly using a custom resolver so the result is
// independent of the system-wide DNS configuration.
func CheckDNS(domain string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "udp", constants.LocalhostIP+":53")
		},
	}

	addrs, err := resolver.LookupHost(ctx, domain)
	if err != nil {
		return false
	}
	return slices.Contains(addrs, constants.LocalhostIP)
}

// CheckSystemDNS tests if the system's default resolver resolves the given
// domain to localhost.
func CheckSystemDNS(domain string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	addrs, err := net.DefaultResolver.LookupHost(ctx, domain)
	if err != nil {
		return false
	}
	return slices.Contains(addrs, constants.LocalhostIP)
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
// It delegates to updateSystemdResolvedConfig with the current domain list.
func setupSystemdResolved() error {
	domains, err := LoadLocalDomains()
	if err != nil {
		domains = []string{}
	}
	return updateSystemdResolvedConfig(domains)
}

// updateSystemdResolvedConfig writes /etc/systemd/resolved.conf.d/srv-local.conf
// so that systemd-resolved routes queries for each registered local domain
// (and the standard local TLDs) through dnsmasq on 127.0.0.1:53.
// It is called whenever the domain list changes.
func updateSystemdResolvedConfig(domains []string) error {
	configFile := constants.SystemdResolvedConfigPath
	configDir := filepath.Dir(configFile)

	// Build the Domains= value: standard local TLDs plus one entry per
	// registered domain. The ~ prefix tells systemd-resolved to route
	// matching queries to this DNS server rather than to the default.
	routingDomains := make([]string, 0, len(LocalDomains)+len(domains))
	for _, tld := range LocalDomains {
		routingDomains = append(routingDomains, "~"+tld)
	}
	for _, d := range domains {
		routingDomains = append(routingDomains, "~"+d)
	}

	content := fmt.Sprintf("[Resolve]\nDNS=%s\nDomains=%s\n",
		constants.LocalhostIP,
		strings.Join(routingDomains, " "),
	)

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

// FlushDNSCache flushes the system DNS cache using the appropriate mechanism
// for the detected resolver. Called after updating DNS routing config so the
// new domain takes effect immediately without waiting for cache TTLs.
func FlushDNSCache() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch DetectResolver() {
	case ResolverSystemdResolved:
		// systemd-resolved is already restarted in updateSystemdResolvedConfig,
		// but resolvectl flush-caches clears any remaining per-link caches.
		if shell.Exists("resolvectl") {
			_, _ = shell.RunQuietWithContext(ctx, "resolvectl", "flush-caches")
		}
	case ResolverMacOS:
		_, _ = shell.RunQuietWithContext(ctx, "dscacheutil", "-flushcache")
		_, _ = shell.RunQuietWithContext(ctx, "killall", "-HUP", "mDNSResponder")
	case ResolverNetworkManager:
		if shell.Exists("resolvectl") {
			_, _ = shell.RunQuietWithContext(ctx, "resolvectl", "flush-caches")
		}
	}
}

// setupMacOSResolver configures macOS resolver for local domains.
// It delegates to updateMacOSResolverConfig with the current domain list.
func setupMacOSResolver() error {
	domains, err := LoadLocalDomains()
	if err != nil {
		domains = []string{}
	}
	return updateMacOSResolverConfig(domains)
}

// updateMacOSResolverConfig ensures /etc/resolver/<name> files exist for every
// local TLD and every registered domain. macOS consults /etc/resolver/ per
// file name — each file routes queries for that name through the listed
// nameserver, so dev.com needs its own /etc/resolver/dev.com file.
// Files for domains that are no longer registered are removed.
func updateMacOSResolverConfig(domains []string) error {
	if err := shell.SudoMkdir(constants.MacOSResolverDir); err != nil {
		return fmt.Errorf("failed to create resolver directory: %w", err)
	}

	nameserver := "nameserver " + constants.LocalhostIP + "\n"

	// Build the full set of names that should have resolver files.
	wanted := make(map[string]struct{})
	for _, tld := range LocalDomains {
		wanted[tld] = struct{}{}
	}
	for _, d := range domains {
		wanted[d] = struct{}{}
	}

	// Write a resolver file for each wanted name.
	for name := range wanted {
		resolverFile := filepath.Join(constants.MacOSResolverDir, name)
		if data, err := os.ReadFile(resolverFile); err == nil {
			if strings.Contains(string(data), constants.LocalhostIP) {
				continue // Already correct.
			}
		}
		if err := shell.SudoWrite(resolverFile, nameserver); err != nil {
			return fmt.Errorf("failed to write resolver file for %s: %w", name, err)
		}
	}

	// Remove resolver files for domains no longer registered (but keep TLD files).
	entries, err := os.ReadDir(constants.MacOSResolverDir)
	if err != nil {
		return nil // Non-fatal if we can't read the directory.
	}
	for _, entry := range entries {
		name := entry.Name()
		if _, ok := wanted[name]; ok {
			continue
		}
		// Only remove files that contain our nameserver line — don't touch
		// files written by other tools.
		resolverFile := filepath.Join(constants.MacOSResolverDir, name)
		if data, err := os.ReadFile(resolverFile); err == nil {
			if strings.Contains(string(data), constants.LocalhostIP) {
				_ = shell.SudoRemove(resolverFile)
			}
		}
	}

	return nil
}

// setupNetworkManager configures NetworkManager to use local DNS for local domains.
// It delegates to updateNetworkManagerConfig with the current domain list.
func setupNetworkManager() error {
	domains, err := LoadLocalDomains()
	if err != nil {
		domains = []string{}
	}
	return updateNetworkManagerConfig(domains)
}

// updateNetworkManagerConfig writes /etc/NetworkManager/dnsmasq.d/srv-local.conf
// so that NetworkManager's built-in dnsmasq routes queries for each registered
// domain through srv's dnsmasq on 127.0.0.1:53.
func updateNetworkManagerConfig(domains []string) error {
	configFile := constants.NetworkManagerConfigPath
	configDir := filepath.Dir(configFile)

	var content strings.Builder
	content.WriteString("# srv local DNS configuration\n")
	for _, tld := range LocalDomains {
		fmt.Fprintf(&content, "server=/%s/%s\n", tld, constants.LocalhostIP)
	}
	for _, d := range domains {
		fmt.Fprintf(&content, "server=/%s/%s\n", d, constants.LocalhostIP)
	}

	if err := shell.SudoMkdir(configDir); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	if err := shell.SudoWrite(configFile, content.String()); err != nil {
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
		// Remove all resolver files that contain our nameserver line.
		// This covers both local TLD files and per-domain files (e.g. dev.com).
		entries, err := os.ReadDir(constants.MacOSResolverDir)
		if err != nil {
			return nil // Already removed or directory doesn't exist.
		}
		var lastErr error
		for _, entry := range entries {
			resolverFile := filepath.Join(constants.MacOSResolverDir, entry.Name())
			if data, readErr := os.ReadFile(resolverFile); readErr == nil {
				if strings.Contains(string(data), constants.LocalhostIP) {
					if removeErr := shell.SudoRemove(resolverFile); removeErr != nil {
						lastErr = removeErr
					}
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
	defer func() { _ = file.Close() }()

	var domains []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		domain := strings.TrimSpace(scanner.Text())
		if domain != "" && !strings.HasPrefix(domain, "#") {
			domains = append(domains, domain)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading local-domains file: %w", err)
	}
	return domains, nil
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
	if slices.Contains(domains, domain) {
		return nil // Already registered
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

	// Automatically set up system DNS when adding the first local domain.
	// Failure here is non-fatal: the domain is registered in dnsmasq and the
	// caller can still proceed; the user can run `srv dns setup` manually.
	if isFirstDomain && !CheckSystemDNS(domain) {
		if err := SetupDNS(); err != nil {
			// Log but do not propagate — DNS registration succeeded above.
			fmt.Fprintf(os.Stderr, "warning: system DNS setup failed (run 'srv dns setup' manually): %v\n", err)
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

	// Automatically remove system DNS when removing the last local domain.
	// Failure here is non-fatal: the domain was already removed from the
	// registry. The user can run `srv dns remove` manually if needed.
	if len(filtered) == 0 {
		if err := RemoveDNS(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: system DNS removal failed (run 'srv dns remove' manually): %v\n", err)
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
			// Use host-record instead of address= so that only the exact
			// domain resolves to 127.0.0.1.  The address= directive matches
			// the domain AND all subdomains; host-record creates an A record
			// for the precise name only (like an /etc/hosts entry).
			fmt.Fprintf(&content, "host-record=%s,127.0.0.1\n", domain)
		}
	}

	content.WriteString("\n# Forward all other queries to upstream DNS\n")
	upstreamDNS := []string{constants.GoogleDNS1, constants.GoogleDNS2}
	if userCfg, ucErr := cfg.LoadUserConfig(); ucErr == nil && len(userCfg.UpstreamDNS) > 0 {
		upstreamDNS = userCfg.UpstreamDNS
	}
	for _, server := range upstreamDNS {
		fmt.Fprintf(&content, "server=%s\n", server)
	}
	content.WriteString("\n# Don't read /etc/resolv.conf\n")
	content.WriteString("no-resolv\n")

	dnsmasqPath := filepath.Join(cfg.TraefikDir, constants.DnsmasqConfFile)
	if err := os.WriteFile(dnsmasqPath, []byte(content.String()), constants.FilePermDefault); err != nil {
		return fmt.Errorf("failed to write dnsmasq.conf: %w", err)
	}

	// Keep the system resolver routing config in sync so that every registered
	// domain (not just .test/.local/.localhost TLDs) is routed through dnsmasq.
	switch DetectResolver() {
	case ResolverSystemdResolved:
		if _, err := os.Stat(constants.SystemdResolvedConfigPath); err == nil {
			if err := updateSystemdResolvedConfig(domains); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to update systemd-resolved config: %v\n", err)
			}
		}
	case ResolverNetworkManager:
		if _, err := os.Stat(constants.NetworkManagerConfigPath); err == nil {
			if err := updateNetworkManagerConfig(domains); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to update NetworkManager config: %v\n", err)
			}
		}
	case ResolverMacOS:
		if _, err := os.Stat(constants.MacOSResolverDir); err == nil {
			if err := updateMacOSResolverConfig(domains); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to update macOS resolver config: %v\n", err)
			}
		}
	}

	// Flush system DNS cache so the new routing takes effect immediately.
	FlushDNSCache()

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
