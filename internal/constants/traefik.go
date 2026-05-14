package constants

// =============================================================================
// Traefik Constants
// =============================================================================

const (
	// EntryPointWeb is the Traefik HTTP entrypoint name.
	EntryPointWeb = "web"
	// EntryPointWebsecure is the Traefik HTTPS entrypoint name.
	EntryPointWebsecure = "websecure"
	// EntryPointInternal is the plain-HTTP entrypoint used for in-cluster /
	// host-internal traffic that bypasses TLS.
	EntryPointInternal = "internal"
	// ListenerInternal is the metadata.yml `listeners` entry that maps to the
	// internal entrypoint.
	ListenerInternal = "internal"
	// CertResolverLetsEncrypt is the Let's Encrypt certificate resolver name.
	CertResolverLetsEncrypt = "letsencrypt"
	// SiteConfigPrefix is the prefix for site configuration files.
	SiteConfigPrefix = "site-"
	// ProxyConfigPrefix is the prefix for proxy configuration files.
	ProxyConfigPrefix = "proxy-"
	// RoutesConfigPrefix is the prefix for per-site extra route configuration files.
	RoutesConfigPrefix = "routes-"
	// ComposeProjectName groups every srv-managed compose project under one
	// umbrella so `docker compose ls` aggregates them into a single row.
	ComposeProjectName = "srv"
	// LabelSrvSite is the Docker label attached to every srv-managed container.
	LabelSrvSite = "dev.srv.site"
	// LabelSrvType is the Docker label identifying the site type.
	LabelSrvType = "dev.srv.type"
	// TraefikDashboardDomain is the local domain used to expose the Traefik dashboard over HTTPS.
	TraefikDashboardDomain = "traefik.local"
	// TraefikDashboardProxyName is the proxy name used for the dashboard proxy.
	TraefikDashboardProxyName = "traefik"
	// NetworkSuffix is the suffix for the Docker network name.
	NetworkSuffix = "_traefik"
	// NetworkHashLength is the length of the hash in the network name.
	NetworkHashLength = 12
)

// Traefik binding addresses.
const (
	// BindHTTP is the HTTP binding address.
	BindHTTP = ":80"
	// BindHTTPS is the HTTPS binding address.
	BindHTTPS = ":443"
	// BindInternal is the binding address for the plain-HTTP internal entrypoint.
	BindInternal = ":88"
)

// Traefik port mappings for docker-compose.
const (
	// PortMapHTTP is the HTTP port mapping.
	PortMapHTTP = "80:80"
	// PortMapHTTPS is the HTTPS port mapping.
	PortMapHTTPS = "443:443"
	// PortMapDashboard is the dashboard port mapping.
	PortMapDashboard = "8080:8080"
	// PortMapDNS is the DNS port mapping (localhost only).
	PortMapDNS = "127.0.0.1:53:53/udp"
)

// =============================================================================
// Certificate Constants
// =============================================================================

const (
	// CertExpiryWarningDays is the number of days before expiry to show warnings.
	CertExpiryWarningDays = 30
	// HoursPerDay is the number of hours in a day.
	HoursPerDay = 24
)

// =============================================================================
// SSL Types
// =============================================================================

const (
	// TypeLabelLocal is the label for local SSL.
	TypeLabelLocal = "local"
	// TypeLabelProduction is the label for production SSL.
	TypeLabelProduction = "production"
)

// =============================================================================
// DNS Constants
// =============================================================================

const (
	// GoogleDNS1 is the primary Google public DNS server.
	GoogleDNS1 = "8.8.8.8"
	// GoogleDNS2 is the secondary Google public DNS server.
	GoogleDNS2 = "8.8.4.4"
	// DNSTestDomain is the domain used for testing DNS resolution.
	DNSTestDomain = "test.test"
)

// =============================================================================
// Mkcert Constants
// =============================================================================

const (
	// MkcertInstallMac is the macOS mkcert installation command.
	MkcertInstallMac = "brew install mkcert"
	// MkcertInstallURL is the mkcert installation documentation URL.
	MkcertInstallURL = "https://github.com/FiloSottile/mkcert#installation"
)

// =============================================================================
// Firewall Constants
// =============================================================================

const (
	// UFWActiveStatus is the string indicating UFW is active.
	UFWActiveStatus = "Status: active"
	// FirewalldRunning is the string indicating firewalld is running.
	FirewalldRunning = "running"
)
