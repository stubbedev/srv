// Package constants — runtime.go holds the per-language constants used when
// srv generates Dockerfiles, compose files, and nginx configs for sites it
// owns directly: PHP, Node.js (incl. Bun/Deno), Ruby, Python, and the
// catch-all Dockerfile site type.
package constants

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
	// PHPFPMConfFile is the FPM pool config filename generated for PHP sites.
	PHPFPMConfFile = "php-fpm.conf"
	// PHPFPMConfContainerPath is the path inside the PHP container where the
	// FPM pool override is mounted. Filename starts with "zz-" so it sorts
	// after the base image's www.conf and overrides it.
	PHPFPMConfContainerPath = "/usr/local/etc/php-fpm.d/zz-srv.conf"
	// PHPDockerRootPath is the working directory inside PHP/nginx containers.
	// With the shared FPM pool, each site is mounted at /var/www/<sitename>
	// rather than the legacy single-site /var/www/html. The constant is kept
	// for code paths that still hard-code the directory; new code uses
	// PHPSiteDocRootRoot + "/" + <sitename>.
	PHPDockerRootPath = "/var/www/html"
	// PHPSiteDocRootRoot is the parent directory inside the shared FPM
	// container under which each member site's project is mounted at its
	// site name (e.g. /var/www/kontainer).
	PHPSiteDocRootRoot = "/var/www"
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
