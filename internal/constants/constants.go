// Package constants provides shared constants used across the srv application.
package constants

import (
	"os"
	"time"
)

// =============================================================================
// Application Metadata
// =============================================================================

const (
	// AppName is the name of the application.
	AppName = "srv"
	// DefaultVersion is the default version when not set by build.
	DefaultVersion = "dev"
	// DefaultCommit is the default commit hash when not set by build.
	DefaultCommit = "none"
	// DefaultBuildDate is the default build date when not set by build.
	DefaultBuildDate = "unknown"
)

// =============================================================================
// Exit Codes
// =============================================================================

const (
	// ExitCodeSuccess indicates successful execution.
	ExitCodeSuccess = 0
	// ExitCodeError indicates an error occurred.
	ExitCodeError = 1
)

// =============================================================================
// Network Constants
// =============================================================================

const (
	// LocalhostIP is the localhost IP address.
	LocalhostIP = "127.0.0.1"
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
	// PortDNS is the DNS server port.
	PortDNS = 53
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
	PortDNSStr       = "53"
)

// Port name constants for display purposes.
const (
	PortNameHTTP      = "HTTP"
	PortNameHTTPS     = "HTTPS"
	PortNameDashboard = "Dashboard"
	PortNameDNS       = "DNS"
)

// =============================================================================
// File Permissions
// =============================================================================

// File and directory permission constants.
const (
	// FilePermDefault is the default permission for regular files (rw-r--r--).
	FilePermDefault os.FileMode = 0o644
	// FilePermACME is the permission for ACME certificate files (rw-------).
	FilePermACME os.FileMode = 0o600
	// DirPermDefault is the default permission for directories (rwxr-xr-x).
	DirPermDefault os.FileMode = 0o755
)

// =============================================================================
// File Names and Extensions
// =============================================================================

const (
	// DockerComposeFile is the standard docker-compose filename.
	DockerComposeFile = "docker-compose.yml"
	// MetadataFile is the filename for site metadata.
	MetadataFile = "metadata.yml"
	// NginxConfFile is the nginx configuration filename.
	NginxConfFile = "nginx.conf"
	// UserConfigFile is the user configuration filename.
	UserConfigFile = "config.yml"
	// EnvTraefikFile is the Traefik environment file.
	EnvTraefikFile = "env.traefik"
	// LocalDomainsFile is the local domains registry file.
	LocalDomainsFile = "local-domains.txt"
	// RootCAFile is the mkcert root CA filename.
	RootCAFile = "rootCA.pem"
	// ACMEJSONFile is the ACME certificate storage file.
	ACMEJSONFile = "acme.json"
	// DnsmasqConfFile is the dnsmasq configuration file.
	DnsmasqConfFile = "dnsmasq.conf"
)

// File extensions.
const (
	// ExtCert is the certificate file extension.
	ExtCert = ".crt"
	// ExtKey is the key file extension.
	ExtKey = ".key"
	// ExtYAML is the YAML file extension.
	ExtYAML = ".yml"
	// ExtTmp is the temporary file extension.
	ExtTmp = ".tmp"
)

// =============================================================================
// Directory Names
// =============================================================================

const (
	// TraefikSubdir is the traefik subdirectory name.
	TraefikSubdir = "traefik"
	// SitesSubdir is the sites subdirectory name.
	SitesSubdir = "sites"
	// CertsSubdir is the certificates subdirectory name.
	CertsSubdir = "certs"
	// LogsSubdir is the logs subdirectory name.
	LogsSubdir = "logs"
	// ConfSubdir is the configuration subdirectory name.
	ConfSubdir = "conf"
	// DefaultConfigDir is the default config directory under home.
	DefaultConfigDir = ".config"
)

// =============================================================================
// Environment Variables
// =============================================================================

const (
	// EnvSrvRoot is the environment variable for the srv root directory.
	EnvSrvRoot = "SRV_ROOT"
	// EnvXDGConfigHome is the XDG config home environment variable.
	EnvXDGConfigHome = "XDG_CONFIG_HOME"
	// EnvACMEEmail is the environment variable prefix for ACME email.
	EnvACMEEmail = "ACME_EMAIL"
)

// =============================================================================
// Traefik Constants
// =============================================================================

const (
	// EntryPointWeb is the Traefik HTTP entrypoint name.
	EntryPointWeb = "web"
	// EntryPointWebsecure is the Traefik HTTPS entrypoint name.
	EntryPointWebsecure = "websecure"
	// CertResolverLetsEncrypt is the Let's Encrypt certificate resolver name.
	CertResolverLetsEncrypt = "letsencrypt"
	// SiteConfigPrefix is the prefix for site configuration files.
	SiteConfigPrefix = "site-"
	// ProxyConfigPrefix is the prefix for proxy configuration files.
	ProxyConfigPrefix = "proxy-"
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

// =============================================================================
// Status Strings
// =============================================================================

const (
	// StatusRunning indicates a container/service is running.
	StatusRunning = "running"
	// StatusStopped indicates a container/service is stopped.
	StatusStopped = "stopped"
	// StatusBroken indicates a site is broken.
	StatusBroken = "broken"
	// StatusPartial indicates partial status.
	StatusPartial = "partial"
)

// Container status strings.
const (
	// StatusPrefixUp is the prefix for running container status.
	StatusPrefixUp = "Up"
)

// =============================================================================
// Docker Status Formats
// =============================================================================

const (
	// ComposeStatusFormat is the format string for docker compose status.
	ComposeStatusFormat = "{{.Status}}"
	// InspectRunningFormat is the format string for inspecting container running state.
	InspectRunningFormat = "{{.State.Running}}"
	// TrueString is the string representation of true.
	TrueString = "true"
)

// Docker error message substrings.
const (
	// ErrAlreadyExists is the error message when a resource already exists.
	ErrAlreadyExists = "already exists"
	// ErrEndpointExists is the error message when an endpoint already exists.
	ErrEndpointExists = "endpoint with name"
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
// Site Types
// =============================================================================

const (
	// SiteTypeCompose is the Docker compose site type.
	SiteTypeCompose = "compose"
	// SiteTypeStatic is the static site type.
	SiteTypeStatic = "static"
)

// =============================================================================
// Proxy Types
// =============================================================================

const (
	// ProxyTypeLocalhost indicates proxy to localhost.
	ProxyTypeLocalhost = "localhost"
	// ProxyTypeContainer indicates proxy to a container.
	ProxyTypeContainer = "container"
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
	// SchemeHTTPSPrefix is the HTTPS URL scheme prefix.
	SchemeHTTPSPrefix = "https://"
)

// =============================================================================
// Nginx Constants
// =============================================================================

const (
	// ImageNginxAlpine is the nginx alpine Docker image.
	ImageNginxAlpine = "nginx:alpine"
	// NginxPort is the default nginx listen port.
	NginxPort = 80
	// NginxHTMLPath is the nginx static files path.
	NginxHTMLPath = "/usr/share/nginx/html"
	// NginxDefaultConfPath is the nginx default configuration path.
	NginxDefaultConfPath = "/etc/nginx/conf.d/default.conf"
	// RestartUnlessStopped is the Docker restart policy.
	RestartUnlessStopped = "unless-stopped"
	// GzipMinLength is the minimum content length for gzip compression.
	GzipMinLength = 1024
	// CacheExpiry is the default cache expiry duration string.
	CacheExpiry = "1y"
)

// =============================================================================
// Static Site Container Constants
// =============================================================================

const (
	// StaticContainerPrefix is the prefix for static site container names.
	StaticContainerPrefix = "srv_static_"
	// StaticContainerHashLength is the hash length in static container names.
	StaticContainerHashLength = 8
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
// Tunnel Tools
// =============================================================================

const (
	// ToolCloudflared is the cloudflared tunnel tool name.
	ToolCloudflared = "cloudflared"
	// ToolNgrok is the ngrok tunnel tool name.
	ToolNgrok = "ngrok"
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

// =============================================================================
// System Paths
// =============================================================================

const (
	// SystemdResolvePath is the path to systemd-resolved configuration.
	SystemdResolvePath = "/run/systemd/resolve/stub-resolv.conf"
	// HomeDirPrefix is the home directory prefix.
	HomeDirPrefix = "~/"
)

// =============================================================================
// DNS Configuration Paths
// =============================================================================

const (
	// SystemdResolvedConfigPath is the systemd-resolved config file path for srv.
	SystemdResolvedConfigPath = "/etc/systemd/resolved.conf.d/srv-local.conf"
	// NetworkManagerConfigPath is the NetworkManager dnsmasq config file path for srv.
	NetworkManagerConfigPath = "/etc/NetworkManager/dnsmasq.d/srv-local.conf"
	// MacOSResolverDir is the macOS resolver directory.
	MacOSResolverDir = "/etc/resolver"
)

// =============================================================================
// Traefik Container Paths
// =============================================================================

const (
	// TraefikContainerSitesPath is the mount point for sites inside the Traefik container.
	TraefikContainerSitesPath = "/etc/traefik/sites"
	// TraefikContainerCertsSubdir is the certs subdirectory name inside sites.
	TraefikContainerCertsSubdir = "certs"
)

// =============================================================================
// Date Formats
// =============================================================================

const (
	// DateFormat is the standard date format.
	DateFormat = "2006-01-02"
)

// =============================================================================
// Timeout Constants
// =============================================================================

const (
	// SpinnerTimeout is the maximum duration for a spinner before auto-stop.
	SpinnerTimeout = 10 * time.Minute
	// SpinnerInterval is the animation interval for spinners.
	SpinnerInterval = 100 * time.Millisecond
)

// =============================================================================
// Batch Operation Constants
// =============================================================================

const (
	// MaxWorkers is the maximum number of parallel workers for batch operations.
	MaxWorkers = 4
	// MaxStatusWorkers is the maximum number of workers for status checks.
	MaxStatusWorkers = 8
)

// =============================================================================
// UI Constants
// =============================================================================

const (
	// IndentString is the standard indentation string.
	IndentString = "  "
	// TableSeparator is the Unicode table separator character.
	TableSeparator = "─"
	// AccessLogBufferSize is the buffer size for access logs.
	AccessLogBufferSize = 100
)

// =============================================================================
// Random String Constants
// =============================================================================

const (
	// DNSUserLength is the length of the DNS HTTP user string.
	DNSUserLength = 16
	// DNSPassLength is the length of the DNS HTTP password string.
	DNSPassLength = 32
)

// =============================================================================
// Default Values
// =============================================================================

const (
	// DefaultHostname is the default hostname when hostname cannot be determined.
	DefaultHostname = "default"
	// DefaultContainerPort is the default container port.
	DefaultContainerPort = 80
)

// =============================================================================
// Initialization Steps
// =============================================================================

const (
	// InitBaseSteps is the base number of initialization steps.
	InitBaseSteps = 3
)
