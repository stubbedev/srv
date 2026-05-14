package constants

import "os"

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
	// MetadataSchemaURL is the JSON Schema URL stamped into generated metadata.yml
	// files so editors (yaml-language-server) provide completion + validation.
	MetadataSchemaURL = "https://raw.githubusercontent.com/stubbedev/srv/master/schemas/metadata.schema.json"
	// UserConfigSchemaURL is the JSON Schema URL stamped into generated config.yml files.
	UserConfigSchemaURL = "https://raw.githubusercontent.com/stubbedev/srv/master/schemas/config.schema.json"
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
