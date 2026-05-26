// Package constants — runtime.go holds the constants used when srv generates
// docker-compose configs for the two site types it owns directly: static
// (nginx-fronted) and dockerfile (user-provided Dockerfile). Language
// runtimes (PHP/Node/Ruby/Python) are user-owned — write your own
// Dockerfile or docker-compose.yml and srv will attach Traefik routing on
// top.
package constants

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
