package constants

// =============================================================================
// Network Constants
// =============================================================================

const (
	// LocalhostIP is the localhost IP address.
	LocalhostIP    = "127.0.0.1"
	LocalhostAlias = "localhost"
	// DockerHostInternal is the hostname for reaching the host from inside a Docker container.
	DockerHostInternal = "host.docker.internal"
)

// =============================================================================
// Port Constants
// =============================================================================

const (
	// PortHTTP is the standard HTTP port.
	PortHTTP = 80
	// PortHTTPS is the standard HTTPS port.
	PortHTTPS = 443
	// PortDashboard is the Traefik dashboard port.
	PortDashboard = 8080
	// PortInternal is the plain-HTTP "internal" entrypoint port. Used by sites
	// that opt into the internal listener (typically for container→host calls
	// that skip TLS verification).
	PortInternal = 88
	// PortDNS is the port of srv's embedded DNS server. It is deliberately
	// unprivileged: the daemon runs as a user service (systemd user unit,
	// launchd agent), which cannot bind 53 without a sysctl or capability.
	// The system resolver is pointed at 127.0.0.1:<PortDNS> explicitly
	// (systemd-resolved DNS=, NetworkManager server=, macOS resolver port),
	// so neither the well-known port 53 nor mDNS's 5353 is required. 15353
	// is high, unassigned, and keeps the DNS association readable.
	PortDNS = 15353
	// PortMin is the minimum valid port number.
	PortMin = 1
	// PortMax is the maximum valid port number.
	PortMax = 65535
)

// Port string constants for use in string contexts.
const (
	PortHTTPStr      = "80"
	PortHTTPSStr     = "443"
	PortDashboardStr = "8080"
	PortInternalStr  = "88"
	PortDNSStr       = "15353"
)

// Port name constants for display purposes.
const (
	PortNameHTTP      = "HTTP"
	PortNameHTTPS     = "HTTPS"
	PortNameInternal  = "Internal"
	PortNameDashboard = "Dashboard"
	PortNameDNS       = "DNS"
)

// =============================================================================
// Scheme Strings
// =============================================================================

const (
	// SchemeHTTP is the HTTP URL scheme.
	SchemeHTTP = "http"
	// SchemeHTTPS is the HTTPS URL scheme.
	SchemeHTTPS = "https"
	// SchemeHTTPPrefix is the HTTP URL scheme prefix.
	SchemeHTTPPrefix = "http://"
)

// =============================================================================
// Validation Constants
// =============================================================================

const (
	// MaxDomainLength is the maximum length of a domain name.
	MaxDomainLength = 253
	// MaxDomainLabelLength is the maximum length of a domain label.
	MaxDomainLabelLength = 63
	// MaxSiteNameLength is the maximum length of a site name.
	MaxSiteNameLength = 63
)
