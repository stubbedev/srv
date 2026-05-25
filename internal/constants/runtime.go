// Package constants — runtime.go holds the per-language constants used when
// srv generates docker-compose configs for sites it owns directly (Node,
// Ruby, Python, Dockerfile). PHP runtimes are no longer managed by srv —
// users bring their own Dockerfile or scaffold one with `srv scaffold php`.
package constants

// =============================================================================
// PHP Constants (used only by the scaffold command + detection)
// =============================================================================

const (
	// FrankenPHPImageLatest is the FrankenPHP alpine Docker image (latest stable PHP).
	FrankenPHPImageLatest = "dunglas/frankenphp:alpine"
	// FrankenPHPImageFormat is the format string for versioned FrankenPHP alpine images.
	FrankenPHPImageFormat = "dunglas/frankenphp:php%s-alpine"
	// PHPVersionLatest is the sentinel value meaning "use the latest Docker tag".
	PHPVersionLatest = "latest"
)

// PHP framework identifiers (used by detection + scaffold templates).
const (
	PHPFrameworkLaravel   = "laravel"
	PHPFrameworkSymfony   = "symfony"
	PHPFrameworkWordPress = "wordpress"
	PHPFrameworkGeneric   = "generic"
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
// Dockerfile Constants
// =============================================================================

const (
	// DockerfileDefaultPort is the default port when EXPOSE is not found.
	DockerfileDefaultPort = 3000
	// DockerfileFile is the Dockerfile filename to look for.
	DockerfileFile = "Dockerfile"
)
