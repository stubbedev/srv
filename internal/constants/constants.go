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
	PortInternalStr  = "88"
	PortDNSStr       = "53"
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
	// EnvDNSHTTPUser is the environment variable for the dnsmasq HTTP user.
	EnvDNSHTTPUser = "DNS_HTTP_USER"
	// EnvDNSHTTPPass is the environment variable for the dnsmasq HTTP password.
	EnvDNSHTTPPass = "DNS_HTTP_PASS"
)

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
	// SiteTypePHP is the PHP/FPM site type.
	SiteTypePHP = "php"
	// SiteTypeNode is the Node.js site type.
	SiteTypeNode = "node"
	// SiteTypeRuby is the Ruby site type.
	SiteTypeRuby = "ruby"
	// SiteTypePython is the Python site type.
	SiteTypePython = "python"
	// SiteTypeDockerfile is the Dockerfile site type.
	SiteTypeDockerfile = "dockerfile"
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
)

// =============================================================================
// PHP Constants
// =============================================================================

const (
	// PHPFPMImageLatest is the PHP-FPM alpine Docker image (latest stable).
	PHPFPMImageLatest = "php:fpm-alpine"
	// PHPFPMImageFormat is the format string for versioned PHP-FPM alpine images.
	PHPFPMImageFormat = "php:%s-fpm-alpine"
	// PHPFPMPort is the PHP-FPM FastCGI port.
	PHPFPMPort = 9000
	// PHPFPMServiceName is the service name for the PHP-FPM container in compose.
	PHPFPMServiceName = "php"
	// PHPWebServiceName is the service name for the nginx container in compose.
	PHPWebServiceName = "web"
	// PHPDockerfileFile is the Dockerfile filename generated for PHP sites.
	PHPDockerfileFile = "Dockerfile"
	// PHPIniFile is the php.ini filename generated for PHP sites.
	PHPIniFile = "php.ini"
	// PHPIniContainerPath is the path inside the PHP container where php.ini is mounted.
	PHPIniContainerPath = "/usr/local/etc/php/conf.d/srv-overrides.ini"
	// PHPDockerRootPath is the working directory inside PHP/nginx containers.
	PHPDockerRootPath = "/var/www/html"
	// PHPVersionLatest is the sentinel value meaning "use the latest Docker tag".
	PHPVersionLatest = "latest"
)

// PHP framework identifiers.
const (
	// PHPFrameworkLaravel is the Laravel framework identifier.
	PHPFrameworkLaravel = "laravel"
	// PHPFrameworkSymfony is the Symfony framework identifier.
	PHPFrameworkSymfony = "symfony"
	// PHPFrameworkWordPress is the WordPress framework identifier.
	PHPFrameworkWordPress = "wordpress"
	// PHPFrameworkGeneric is the generic PHP framework identifier.
	PHPFrameworkGeneric = "generic"
)

// =============================================================================
// Node.js Constants
// =============================================================================

const (
	// NodeImageLTS is the Node.js LTS Alpine Docker image.
	NodeImageLTS = "node:lts-alpine"
	// NodeImageFormat is the format string for versioned Node.js Alpine images.
	NodeImageFormat = "node:%s-alpine"
	// BunImageAlpine is the official Bun Alpine Docker image.
	BunImageAlpine = "oven/bun:alpine"
	// DenoImageAlpine is the official Deno Alpine Docker image.
	DenoImageAlpine = "denoland/deno:alpine"
	// NodeVersionLTS is the sentinel value meaning "use the LTS Docker tag".
	NodeVersionLTS = "lts"
	// NodeDefaultPort is the default port for Node.js applications.
	NodeDefaultPort = 3000
	// NodeDockerWorkDir is the working directory inside Node/Bun/Deno containers.
	NodeDockerWorkDir = "/app"
)

// Node.js runtime identifiers.
const (
	// NodeRuntimeNode is the Node.js runtime identifier.
	NodeRuntimeNode = "node"
	// NodeRuntimeBun is the Bun runtime identifier.
	NodeRuntimeBun = "bun"
	// NodeRuntimeDeno is the Deno runtime identifier.
	NodeRuntimeDeno = "deno"
)

// Node.js package manager identifiers.
const (
	// NodePMNPM is the npm package manager identifier.
	NodePMNPM = "npm"
	// NodePMYarn is the Yarn package manager identifier.
	NodePMYarn = "yarn"
	// NodePMPNPM is the pnpm package manager identifier.
	NodePMPNPM = "pnpm"
	// NodePMBun is the Bun package manager identifier.
	NodePMBun = "bun"
	// NodePMDeno is used for Deno projects (manages its own dependencies).
	NodePMDeno = "deno"
)

// Node.js framework identifiers.
const (
	// NodeFrameworkNext is the Next.js framework identifier.
	NodeFrameworkNext = "next"
	// NodeFrameworkNuxt is the Nuxt framework identifier.
	NodeFrameworkNuxt = "nuxt"
	// NodeFrameworkVite is the Vite framework identifier.
	NodeFrameworkVite = "vite"
	// NodeFrameworkExpress is the Express framework identifier.
	NodeFrameworkExpress = "express"
	// NodeFrameworkNestJS is the NestJS framework identifier.
	NodeFrameworkNestJS = "nestjs"
	// NodeFrameworkGeneric is the generic Node.js framework identifier.
	NodeFrameworkGeneric = "generic"
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
// Ruby Constants
// =============================================================================

const (
	// RubyImageAlpine is the official Ruby Alpine Docker image.
	RubyImageAlpine = "ruby:alpine"
	// RubyImageFormat is the format string for versioned Ruby Alpine images.
	RubyImageFormat = "ruby:%s-alpine"
	// RubyVersionLatest is the sentinel value meaning "use the latest tag".
	RubyVersionLatest = "latest"
	// RubyDefaultPort is the default port for Ruby applications.
	RubyDefaultPort = 3000
	// RubyDockerWorkDir is the working directory inside Ruby containers.
	RubyDockerWorkDir = "/app"
)

// Ruby framework identifiers.
const (
	// RubyFrameworkRails is the Rails framework identifier.
	RubyFrameworkRails = "rails"
	// RubyFrameworkSinatra is the Sinatra framework identifier.
	RubyFrameworkSinatra = "sinatra"
	// RubyFrameworkRack is the Rack framework identifier.
	RubyFrameworkRack = "rack"
	// RubyFrameworkGeneric is the generic Ruby framework identifier.
	RubyFrameworkGeneric = "generic"
)

// =============================================================================
// Python Constants
// =============================================================================

const (
	// PythonImageAlpine is the official Python Alpine Docker image.
	PythonImageAlpine = "python:alpine"
	// PythonImageFormat is the format string for versioned Python Alpine images.
	PythonImageFormat = "python:%s-alpine"
	// PythonVersionLatest is the sentinel value meaning "use the latest tag".
	PythonVersionLatest = "latest"
	// PythonDefaultPort is the default port for Python applications.
	PythonDefaultPort = 8000
	// PythonDockerWorkDir is the working directory inside Python containers.
	PythonDockerWorkDir = "/app"
)

// Python framework identifiers.
const (
	// PythonFrameworkDjango is the Django framework identifier.
	PythonFrameworkDjango = "django"
	// PythonFrameworkFastAPI is the FastAPI framework identifier.
	PythonFrameworkFastAPI = "fastapi"
	// PythonFrameworkFlask is the Flask framework identifier.
	PythonFrameworkFlask = "flask"
	// PythonFrameworkGeneric is the generic Python framework identifier.
	PythonFrameworkGeneric = "generic"
)

// =============================================================================
// Dockerfile Constants
// =============================================================================

const (
	// DockerfileDefaultPort is the default port when EXPOSE is not found.
	DockerfileDefaultPort = 3000
	// DockerfileFile is the Dockerfile filename to look for.
	DockerfileFile = "Dockerfile"
)

// =============================================================================
// Initialization Steps
// =============================================================================

const (
	// InitBaseSteps is the base number of initialization steps.
	// network + config + start traefik + dashboard proxy
	InitBaseSteps = 4
)
