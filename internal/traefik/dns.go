package traefik

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/fsutil"
	"github.com/stubbedev/srv/internal/ops"
	"github.com/stubbedev/srv/internal/platform"
	"github.com/stubbedev/srv/internal/shell"
)

// LocalDomains are the TLDs used for local development.
var LocalDomains = []string{"test", "local", "localhost"}

// routingTLDs are the local TLDs srv routes to dnsmasq wholesale. `local` is
// deliberately excluded: it is reserved for mDNS (RFC 6762), so claiming the
// entire `~local` / `/local/` TLD would hijack every LAN mDNS name (other
// hosts, printers, `ssh foo.local`) to dnsmasq. srv instead routes its own
// `.local` domains by exact name (see the resolver builders), leaving the rest
// of `.local` to the system's mDNS resolver.
var routingTLDs = []string{"test", "localhost"}

// isUnderRoutingTLD reports whether bare equals or is a subdomain of a
// TLD-wide-routed local TLD. `.local` names return false so they are routed
// per-name rather than swallowing the whole mDNS TLD.
func isUnderRoutingTLD(bare string) bool {
	for _, tld := range routingTLDs {
		if bare == tld || strings.HasSuffix(bare, "."+tld) {
			return true
		}
	}
	return false
}

// WildcardPrefix marks a registry entry as a wildcard domain (apex + one-level subdomains).
const WildcardPrefix = "*."

// BareDomain strips the wildcard prefix from a registry entry, returning the
// apex domain. Returns the input unchanged for non-wildcard entries.
func BareDomain(entry string) string {
	return strings.TrimPrefix(entry, WildcardPrefix)
}

// IsWildcardEntry reports whether a registry entry is a wildcard.
func IsWildcardEntry(entry string) bool {
	return strings.HasPrefix(entry, WildcardPrefix)
}

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
	if platform.IsDarwin() {
		return ResolverMacOS
	}

	// Check for NetworkManager
	if shell.Exists("nmcli") {
		return ResolverNetworkManager
	}

	return ResolverUnknown
}

// CheckDNS tests if the local DNS server resolves the given domain to localhost.
// It queries srv's embedded DNS server directly using a custom resolver so the
// result is independent of the system-wide DNS configuration.
//
// The argument may be a raw registry entry, so strip any wildcard prefix
// first: Go's resolver rejects the literal `*` label as an invalid hostname
// and fails before sending a query, which reads as "DNS not responding" even
// though dnsmasq's address=/apex/ covers the apex and every subdomain.
func CheckDNS(domain string) bool {
	domain = BareDomain(domain)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "udp", net.JoinHostPort(constants.LocalhostIP, constants.PortDNSStr))
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
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
		return errors.New("unsupported DNS configuration. Please manually configure your system to use 127.0.0.1 for .test, .local, and .localhost domains")
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

// renderResolvedConf builds the resolved.conf drop-in that routes the given
// (already ~-prefixed) domains through srv's embedded DNS server on the
// loopback, at the unprivileged embedded port (see constants.PortDNS — the
// daemon is a user service and cannot bind 53). Written as text: the ini
// shape is fixed and tiny, and the change-detection below compares bytes,
// which a hand writer makes predictable.
func renderResolvedConf(routingDomains []string) string {
	var b strings.Builder
	b.WriteString("[Resolve]\n")
	fmt.Fprintf(&b, "DNS=%s:%s\n", constants.LocalhostIP, constants.PortDNSStr)
	fmt.Fprintf(&b, "Domains=%s\n", strings.Join(routingDomains, " "))
	return b.String()
}

// updateSystemdResolvedConfig writes /etc/systemd/resolved.conf.d/srv-local.conf
// so that systemd-resolved routes queries for each registered local domain
// (and the standard local TLDs) through dnsmasq on 127.0.0.1:53.
// It is called whenever the domain list changes.
//
// Restarting systemd-resolved disrupts DNS for the whole machine, so the
// restart is skipped when the rendered config is byte-identical to what is
// already on disk — which is the common case, since domains under a standard
// local TLD (e.g. foo.test) are already covered by the ~test routing entry.
func updateSystemdResolvedConfig(domains []string) error {
	configFile := constants.SystemdResolvedConfigPath
	configDir := filepath.Dir(configFile)

	// Build the Domains= value: standard local TLDs plus one entry per
	// registered domain that is NOT already covered by a local TLD entry.
	// The ~ prefix tells systemd-resolved to route matching queries to this
	// DNS server rather than to the default.
	routingDomains := make([]string, 0, len(routingTLDs)+len(domains))
	for _, tld := range routingTLDs {
		routingDomains = append(routingDomains, "~"+tld)
	}
	for _, d := range domains {
		bare := BareDomain(d)
		if isUnderRoutingTLD(bare) {
			continue
		}
		// .local domains land here and get a per-name route (~grafana.local),
		// so unrelated .local names still reach mDNS via systemd-resolved.
		routingDomains = append(routingDomains, "~"+bare)
	}

	content := renderResolvedConf(routingDomains)

	// Nothing to do — and crucially, no system-wide DNS restart — when the
	// routing config has not actually changed.
	if existing, err := os.ReadFile(configFile); err == nil && string(existing) == content {
		return nil
	}

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

	// Build the full set of names that should have resolver files. The TLD-wide
	// files cover .test/.localhost; .local is NOT given a TLD-wide
	// /etc/resolver/local file (that would hijack all Bonjour .local names) —
	// each registered .local domain gets its own per-name resolver file below.
	wanted := make(map[string]struct{})
	for _, tld := range routingTLDs {
		wanted[tld] = struct{}{}
	}
	for _, d := range domains {
		wanted[BareDomain(d)] = struct{}{}
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
		return nil //nolint:nilerr // Non-fatal if we can't read the directory.
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
// domain through srv's embedded DNS server on the loopback at the unprivileged
// embedded port (dnsmasq's server= syntax marks the port with '#').
//
// Restarting NetworkManager is disruptive, so it is skipped when the rendered
// config is unchanged — which is the common case, since domains under a
// standard local TLD are already covered by the per-TLD routing entry.
func updateNetworkManagerConfig(domains []string) error {
	configFile := constants.NetworkManagerConfigPath
	configDir := filepath.Dir(configFile)

	var content strings.Builder
	content.WriteString("# srv local DNS configuration\n")
	for _, tld := range routingTLDs {
		fmt.Fprintf(&content, "server=/%s/%s#%s\n", tld, constants.LocalhostIP, constants.PortDNSStr)
	}
	for _, d := range domains {
		bare := BareDomain(d)
		if isUnderRoutingTLD(bare) {
			continue
		}
		// .local domains get a per-name server= line; other .local names stay
		// on mDNS rather than being routed wholesale to srv's DNS server.
		fmt.Fprintf(&content, "server=/%s/%s#%s\n", bare, constants.LocalhostIP, constants.PortDNSStr)
	}

	if existing, err := os.ReadFile(configFile); err == nil && string(existing) == content.String() {
		return nil
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
			return nil //nolint:nilerr // Already removed or directory doesn't exist.
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
	slices.Sort(domains)
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

// dnsConfigMu serializes every writer of the shared DNS state: the
// local-domains registry, dnsmasq.conf, the hostsdir file, and the system
// resolver routing config. These are read-modify-write cycles over files
// shared by all sites, so concurrent site operations (a batch start racing
// the daemon's reload timers) used to each read the registry, append their
// entry, and write their own copy back — silently losing the other's
// registration. Reads stay lock-free: writers publish via atomic rename, so
// a reader sees either the old or the new file, never a torn one.
var dnsConfigMu sync.Mutex

// RegisterLocalDomain adds a domain to the local DNS registry and updates dnsmasq.
// Automatically configures system DNS when the first local domain is added.
// When wildcard is true, the entry is stored as "*.<domain>" so that dnsmasq
// emits an `address=` directive matching the apex and one-level subdomains.
// Registering the same bare domain with a different wildcard setting upgrades
// or downgrades the existing entry.
func RegisterLocalDomain(domain string, wildcard bool) error {
	return RegisterLocalDomains([]string{domain}, wildcard)
}

// RegisterLocalDomains is the batch form of RegisterLocalDomain: the registry
// is loaded and saved once and dnsmasq is regenerated once, no matter how many
// domains a site carries. A multi-domain site add used to run the whole
// regen+flush pipeline once per domain — D container restarts where one
// sufficed when any of them was a wildcard.
func RegisterLocalDomains(domains []string, wildcard bool) error {
	dnsConfigMu.Lock()
	defer dnsConfigMu.Unlock()
	return registerLocalDomainsLocked(domains, wildcard)
}

func registerLocalDomainsLocked(domains []string, wildcard bool) error {
	entries := make([]string, 0, len(domains))
	bare := make(map[string]bool, len(domains))
	for _, d := range domains {
		entry := d
		if wildcard {
			entry = WildcardPrefix + d
		}
		entries = append(entries, entry)
		bare[d] = true
	}

	existing, err := LoadLocalDomains()
	if err != nil {
		return err
	}

	// Drop conflicting alternate-form entries for every bare domain being
	// registered (wildcard supersedes apex-only and vice versa).
	filtered := make([]string, 0, len(existing)+len(entries))
	for _, d := range existing {
		if bare[BareDomain(d)] {
			continue
		}
		filtered = append(filtered, d)
	}

	// All entries already registered in the same form: idempotent no-op, no
	// regen, no flush.
	allPresent := true
	for _, e := range entries {
		if !slices.Contains(filtered, e) {
			allPresent = false
			break
		}
	}
	if allPresent {
		return nil
	}

	// First local domain triggers the one-time system DNS setup below.
	isFirstDomain := len(filtered) == 0

	filtered = append(filtered, entries...)
	if err := SaveLocalDomains(filtered); err != nil {
		return err
	}

	if err := updateDnsmasqConfigLocked(); err != nil {
		return err
	}

	// Automatically set up system DNS when adding the first local domain.
	// Failure here is non-fatal: the domain is registered in dnsmasq and the
	// caller can still proceed; the user can run `srv dns setup` manually.
	if isFirstDomain && !CheckSystemDNS(domains[0]) {
		if err := SetupDNS(); err != nil {
			// Log but do not propagate — DNS registration succeeded above.
			fmt.Fprintf(os.Stderr, "warning: system DNS setup failed (run 'srv dns setup' manually): %v\n", err)
		}
	}

	return nil
}

// UnregisterLocalDomain removes a domain from the local DNS registry and updates dnsmasq.
// Automatically removes system DNS configuration when the last local domain is removed.
// Matches both the bare and the wildcard form ("*.<domain>") so callers don't
// need to know how the entry was originally registered.
func UnregisterLocalDomain(domain string) error {
	dnsConfigMu.Lock()
	defer dnsConfigMu.Unlock()

	domains, err := LoadLocalDomains()
	if err != nil {
		return err
	}

	// Filter out the domain (matching either the bare or wildcard form).
	filtered := make([]string, 0, len(domains))
	found := false
	for _, d := range domains {
		if BareDomain(d) == domain {
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

	if err := updateDnsmasqConfigLocked(); err != nil {
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

// buildDnsmasqConf renders dnsmasq.conf. Only wildcard domains and DNS-alias
// redirects land here (via address= directives); exact local domains go into
// the hostsdir instead. The file name and directives keep the dnsmasq shape —
// it is the on-disk contract the embedded DNS server (internal/dnsd) parses,
// and a familiar format for anyone who reads it by hand.
func buildDnsmasqConf(wildcards []string, aliases []ResolvedAlias, upstreamDNS []string) string {
	var b strings.Builder
	b.WriteString("# Local domains managed by srv\n")
	b.WriteString("# Do not edit manually - changes will be overwritten\n\n")
	b.WriteString("# Exact (non-wildcard) domains are auto-reloaded from this directory\n")
	b.WriteString("hostsdir=/etc/dnsmasq.hosts\n\n")

	if len(wildcards) == 0 {
		b.WriteString("# No wildcard domains registered\n")
	} else {
		b.WriteString("# Wildcard domains — match the apex and every subdomain\n")
		for _, d := range wildcards {
			fmt.Fprintf(&b, "address=/%s/127.0.0.1\n", d)
		}
	}

	// DNS-alias redirects: each source name is pinned to the target's resolved
	// IPv4 address. dnsmasq's --cname directive cannot follow chains to
	// upstream names, so we emit an address= record instead — equivalent to
	// what a CNAME chain would resolve to at request time.
	if len(aliases) > 0 {
		b.WriteString("\n# DNS-alias redirects (srv redirect add --dns-only)\n")
		for _, a := range aliases {
			if a.ResolveErr != nil || a.IP == "" {
				fmt.Fprintf(&b, "# %s -> %s: resolution failed (%v); entry skipped\n", a.Source, a.Target, a.ResolveErr)
				continue
			}
			fmt.Fprintf(&b, "# %s -> %s\n", a.Source, a.Target)
			fmt.Fprintf(&b, "address=/%s/%s\n", a.Source, a.IP)
		}
	}

	b.WriteString("\n# Forward all other queries to upstream DNS\n")
	for _, server := range upstreamDNS {
		fmt.Fprintf(&b, "server=%s\n", server)
	}
	b.WriteString("\n# Don't read /etc/resolv.conf\n")
	b.WriteString("no-resolv\n")
	return b.String()
}

// buildDnsmasqHosts renders the /etc/hosts-format file in the hostsdir. dnsmasq
// auto-reloads this file without a restart. It always carries a header so the
// file is never zero-length — dnsmasq cannot detect a change to an emptied
// file, so removing the last domain still needs a non-empty file to land.
func buildDnsmasqHosts(exact []string) string {
	var b strings.Builder
	b.WriteString("# Local domains managed by srv — auto-reloaded by dnsmasq\n")
	b.WriteString("# Do not edit manually - changes will be overwritten\n")
	for _, d := range exact {
		fmt.Fprintf(&b, "127.0.0.1 %s\n", d)
	}
	return b.String()
}

// fileContentDiffers reports whether the file at path differs from want.
// A missing or unreadable file counts as different.
func fileContentDiffers(path, want string) bool {
	existing, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	return string(existing) != want
}

// UpdateDnsmasqConfig regenerates the dnsmasq config from the registered
// domains. Exact domains are written to the hostsdir and applied with a
// SIGHUP; wildcard domains and upstream servers go into dnsmasq.conf and need
// a container restart. The container is only restarted when dnsmasq.conf
// actually changed, so adding or removing an ordinary site no longer
// interrupts DNS. Safe to call concurrently: the write cycle is serialized
// against the register/unregister paths by dnsConfigMu.
func UpdateDnsmasqConfig() error {
	dnsConfigMu.Lock()
	defer dnsConfigMu.Unlock()
	return updateDnsmasqConfigLocked()
}

// updateDnsmasqConfigLocked is UpdateDnsmasqConfig's body; the caller must
// hold dnsConfigMu.
func updateDnsmasqConfigLocked() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	domains, err := LoadLocalDomains()
	if err != nil {
		return err
	}

	// Split registered domains: wildcards need an address= directive in the
	// main config (restart to apply); exact names go into the auto-reloaded
	// hostsdir (no restart). Entries are pre-sorted by SaveLocalDomains.
	var wildcards, exact []string
	for _, d := range domains {
		if IsWildcardEntry(d) {
			wildcards = append(wildcards, BareDomain(d))
		} else {
			exact = append(exact, d)
		}
	}

	// Through ops: these values are interpolated into dnsmasq.conf as
	// `server=<value>` below, so they must have passed validation. Before the
	// ops layer this read the file directly and a newline in a value could
	// inject arbitrary directives. A config that fails validation falls back to
	// the defaults rather than writing a conf dnsmasq will not start on.
	upstreamDNS := []string{constants.GoogleDNS1, constants.GoogleDNS2}
	if userCfg, ucErr := ops.UserConfig(); ucErr == nil && len(userCfg.UpstreamDNS) > 0 {
		upstreamDNS = userCfg.UpstreamDNS
	}

	// Pick up DNS-alias redirects from redirect-<name>.yml files. Resolution
	// errors land as commented-out entries inside the conf so a single
	// unreachable target degrades gracefully instead of taking down dnsmasq.
	aliasDecls, aliasErr := ScanRedirectAliases()
	if aliasErr != nil {
		// Non-fatal: log and continue with whatever we have.
		fmt.Fprintf(os.Stderr, "warning: failed to scan redirect aliases: %v\n", aliasErr)
	}
	resolvedAliases := ResolveAliases(aliasDecls)

	confBody := buildDnsmasqConf(wildcards, resolvedAliases, upstreamDNS)
	hostsBody := buildDnsmasqHosts(exact)

	dnsmasqPath := filepath.Join(cfg.TraefikDir, constants.DnsmasqConfFile)
	hostsDir := filepath.Join(cfg.TraefikDir, constants.DnsmasqHostsDir)
	hostsPath := filepath.Join(hostsDir, constants.DnsmasqHostsFile)

	// Decide up front what kind of reload each file needs: a change to the
	// main config requires a container restart, a change confined to the
	// hostsdir only needs a SIGHUP. When neither changed, skip everything:
	// writing an unchanged hosts file still makes dnsmasq re-read and flush,
	// and the flush + container inspect below are the expensive tail of every
	// registration.
	confChanged := fileContentDiffers(dnsmasqPath, confBody)
	hostsChanged := fileContentDiffers(hostsPath, hostsBody)
	if !confChanged && !hostsChanged {
		return nil
	}

	if err := os.MkdirAll(hostsDir, constants.DirPermDefault); err != nil {
		return fmt.Errorf("failed to create dnsmasq hosts dir: %w", err)
	}
	// Write atomically: dnsmasq watches the hostsdir (and is SIGHUP'd on a
	// conf change), so a plain truncating write exposes a window where dnsmasq
	// reads a partial file and fails to resolve a domain.
	if hostsChanged {
		if err := fsutil.AtomicWriteFile(hostsPath, []byte(hostsBody), constants.FilePermDefault); err != nil {
			return fmt.Errorf("failed to write dnsmasq hosts file: %w", err)
		}
	}
	if confChanged {
		if err := fsutil.AtomicWriteFile(dnsmasqPath, []byte(confBody), constants.FilePermDefault); err != nil {
			return fmt.Errorf("failed to write dnsmasq.conf: %w", err)
		}
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

	// The embedded DNS server (internal/dnsd, hosted by the daemon) watches
	// both generated files and re-reads them within a fraction of a second.
	// There is no reload signal and no container to restart; when the daemon
	// is not running, the files on disk are the source of truth it loads on
	// next start.
	return nil
}
