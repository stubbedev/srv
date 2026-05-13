// Package site handles site management operations.
package site

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/pool"
)

// =============================================================================
// PHP Site Detection
// =============================================================================

// PHPSiteInfo holds detected configuration for a PHP project.
type PHPSiteInfo struct {
	PHPVersion   string   // "latest" or a specific version like "8.3"
	Extensions   []string // PHP extensions to install
	Framework    string   // laravel, symfony, wordpress, generic
	DocumentRoot string   // Relative path within project (empty = project root)
}

// ComposerJSON represents the structure of a composer.json file.
type ComposerJSON struct {
	Require map[string]string `json:"require"`
}

// DetectPHPSite checks whether dir contains a composer.json and returns
// PHP site info. Returns nil if it is not a PHP/composer project.
func DetectPHPSite(dir string) (*PHPSiteInfo, error) {
	composerPath := filepath.Join(dir, "composer.json")
	data, err := os.ReadFile(composerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading composer.json: %w", err)
	}

	var composer ComposerJSON
	if err := json.Unmarshal(data, &composer); err != nil {
		// Malformed composer.json — treat as raw PHP rather than failing hard.
		return RawPHPDefaults(), nil
	}

	framework := DetectFramework(dir, &composer)
	docRoot := DetectDocumentRoot(dir, framework)
	phpVersion := ParsePHPVersionFromComposer(&composer)
	// Start from the common baseline and merge in any ext-* entries from
	// composer.json. This ensures every site gets a usable set of extensions
	// without requiring every project to exhaustively declare them.
	extensions := InjectFrameworkExtensions(framework, mergeExtensions(BaselinePHPExtensions(), ExtractComposerExtensions(&composer)))

	return &PHPSiteInfo{
		PHPVersion:   phpVersion,
		Extensions:   extensions,
		Framework:    framework,
		DocumentRoot: docRoot,
	}, nil
}

// frameworkExtensions lists extensions that are essential for each framework
// but not always declared in composer.json's ext-* requires.
var frameworkExtensions = map[string][]string{
	constants.PHPFrameworkLaravel: {
		"bcmath",   // used by many Laravel packages and numeric operations
		"fileinfo", // required for file uploads and MIME detection
		"intl",     // used by Carbon, string helpers, and localisation
		"opcache",  // production performance; always worth having
		"pcntl",    // required by queue workers and Horizon
	},
}

// InjectFrameworkExtensions merges the framework-specific essential extensions
// into the provided list, deduplicating and sorting the result.
// This ensures common extensions are always present even when not explicitly
// listed in composer.json's ext-* requires.
func InjectFrameworkExtensions(framework string, extensions []string) []string {
	required, ok := frameworkExtensions[framework]
	if !ok {
		return extensions
	}
	set := make(map[string]bool, len(extensions)+len(required))
	for _, e := range extensions {
		set[e] = true
	}
	for _, e := range required {
		set[e] = true
	}
	result := make([]string, 0, len(set))
	for e := range set {
		result = append(result, e)
	}
	sort.Strings(result)
	return result
}

// mergeExtensions returns a sorted, deduplicated union of two extension slices.
func mergeExtensions(a, b []string) []string {
	set := make(map[string]bool, len(a)+len(b))
	for _, e := range a {
		set[e] = true
	}
	for _, e := range b {
		set[e] = true
	}
	result := make([]string, 0, len(set))
	for e := range set {
		result = append(result, e)
	}
	sort.Strings(result)
	return result
}

// DetectRawPHPSite checks whether dir contains PHP files without a
// composer.json. Returns true if any .php or .phtml file is found at the
// top level of dir.
func DetectRawPHPSite(dir string) (bool, error) {
	// Fast path: check for the most common entry points first.
	for _, name := range []string{"index.php", "index.phtml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true, nil
		}
	}

	// Fallback: scan the top-level directory only (no recursion).
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("reading directory %s: %w", dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if IsPHPFile(entry.Name()) {
			return true, nil
		}
	}
	return false, nil
}

// IsPHPFile returns true if the filename has a .php or .phtml extension.
func IsPHPFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".php" || ext == ".phtml"
}

// RawPHPDefaults returns PHPSiteInfo with the baseline extension set
// and the latest PHP image tag, used when no composer.json is available.
func RawPHPDefaults() *PHPSiteInfo {
	return &PHPSiteInfo{
		PHPVersion:   constants.PHPVersionLatest,
		Extensions:   BaselinePHPExtensions(),
		Framework:    constants.PHPFrameworkGeneric,
		DocumentRoot: "",
	}
}

// BaselinePHPExtensions returns the set of extensions installed for every PHP
// site regardless of what composer.json declares. It covers the most common
// needs out of the box so projects work without having to enumerate every
// ext-* dependency.
func BaselinePHPExtensions() []string {
	return []string{
		// Databases — relational
		"pdo",
		"pdo_mysql",
		"pdo_pgsql",
		"pdo_sqlite",
		"mysqli",
		"pgsql",
		// Databases — NoSQL / cache
		"mongodb",
		"redis",
		// Core string / encoding
		"mbstring",
		"iconv",
		"intl",
		"gettext",
		// XML / HTML
		"xml",
		"simplexml",
		"dom",
		"soap",
		// Networking
		"curl",
		"sockets",
		// File handling
		"zip",
		"fileinfo",
		"ftp",
		// Image processing
		"gd",
		"exif",
		"imagick",
		// Cryptography & security
		"mcrypt",
		"sodium",
		// Math
		"bcmath",
		"gmp",
		// Performance
		"opcache",
		"apcu",
		// Process control (queues, workers)
		"pcntl",
		"posix",
		// Misc commonly needed
		"calendar",
		"shmop",
		"sysvmsg",
		"sysvsem",
		"sysvshm",
	}
}

// =============================================================================
// Framework & Document Root Detection
// =============================================================================

// DetectFramework returns the PHP framework in use based on project files and
// composer dependencies.
func DetectFramework(dir string, composer *ComposerJSON) string {
	// Laravel: has an artisan file at the project root.
	if fileExists(filepath.Join(dir, "artisan")) {
		return constants.PHPFrameworkLaravel
	}

	// WordPress: has wp-config.php or wp-content directory.
	if fileExists(filepath.Join(dir, "wp-config.php")) || dirExists(filepath.Join(dir, "wp-content")) {
		return constants.PHPFrameworkWordPress
	}

	// Symfony: has bin/console and a symfony/* package requirement.
	if fileExists(filepath.Join(dir, "bin", "console")) && hasComposerPackagePrefix(composer, "symfony/") {
		return constants.PHPFrameworkSymfony
	}

	return constants.PHPFrameworkGeneric
}

// DetectDocumentRoot returns the document root path relative to the project
// directory for the given framework.
func DetectDocumentRoot(dir, framework string) string {
	switch framework {
	case constants.PHPFrameworkLaravel:
		if dirExists(filepath.Join(dir, "public")) {
			return "public"
		}
	case constants.PHPFrameworkSymfony:
		// Symfony 4+ uses public/; older versions used web/.
		if dirExists(filepath.Join(dir, "public")) {
			return "public"
		}
		if dirExists(filepath.Join(dir, "web")) {
			return "web"
		}
	case constants.PHPFrameworkWordPress:
		// WordPress is typically served from its root.
		return ""
	default:
		// Generic: prefer public/ or web/ if they contain an index file.
		for _, candidate := range []string{"public", "web", "html"} {
			candidateDir := filepath.Join(dir, candidate)
			if fileExists(filepath.Join(candidateDir, "index.php")) ||
				fileExists(filepath.Join(candidateDir, "index.phtml")) {
				return candidate
			}
		}
	}
	return ""
}

// =============================================================================
// Composer.json Parsing
// =============================================================================

// ParsePHPVersionFromComposer extracts the minimum required PHP version from
// a composer.json Require map. Returns PHPVersionLatest when no PHP
// requirement is declared or the constraint cannot be parsed.
func ParsePHPVersionFromComposer(composer *ComposerJSON) string {
	constraint, ok := composer.Require["php"]
	if !ok || strings.TrimSpace(constraint) == "" {
		return constants.PHPVersionLatest
	}
	if v := parseVersionConstraint(constraint); v != "" {
		return v
	}
	return constants.PHPVersionLatest
}

// ExtractComposerExtensions collects PHP extensions declared in the require
// map (keys beginning with "ext-") and returns them as plain extension names.
func ExtractComposerExtensions(composer *ComposerJSON) []string {
	var exts []string
	for key := range composer.Require {
		if after, ok := strings.CutPrefix(key, "ext-"); ok {
			exts = append(exts, after)
		}
	}
	sort.Strings(exts)
	return exts
}

// parseVersionConstraint converts a composer version constraint string (e.g.
// "^8.3", ">=8.2", "~8.1", "8.3.*") into a simple "major.minor" string
// suitable for use as a Docker image tag. Returns "" on failure.
func parseVersionConstraint(constraint string) string {
	constraint = strings.TrimSpace(constraint)

	// Patterns tried in order: ^8.3 | >=8.2 | >8.2 | ~8.1 | 8.3.0 | 8.3 | 8.3.*
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`^\^(\d+\.\d+)`),
		regexp.MustCompile(`^>=?(\d+\.\d+)`),
		regexp.MustCompile(`^~(\d+\.\d+)`),
		regexp.MustCompile(`^(\d+\.\d+)[\.|\*]?`),
	}
	for _, re := range patterns {
		if m := re.FindStringSubmatch(constraint); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

// =============================================================================
// Extension Parsing (--php-extensions flag)
// =============================================================================

// ParseExtensionOverrides applies the user-provided --php-extensions flag
// value on top of the defaults slice.
//
// Rules:
//   - Empty string → return defaults unchanged.
//   - Any element starting with "+" → add to defaults.
//   - Any element starting with "-" → remove from defaults.
//   - No +/- modifiers at all → treat the whole list as a full replacement.
//   - Mixed modifiers → start from defaults and apply each +/- in order.
func ParseExtensionOverrides(flagValue string, defaults []string) []string {
	if flagValue == "" {
		return defaults
	}

	parts := strings.Split(flagValue, ",")

	// Detect whether any element carries a +/- modifier.
	hasModifiers := false
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "+") || strings.HasPrefix(p, "-") {
			hasModifiers = true
			break
		}
	}

	if !hasModifiers {
		// Full replacement.
		result := make([]string, 0, len(parts))
		for _, ext := range parts {
			ext = strings.TrimSpace(ext)
			if ext != "" {
				result = append(result, ext)
			}
		}
		sort.Strings(result)
		return result
	}

	// Modifier mode: start from defaults and apply each instruction.
	set := make(map[string]bool, len(defaults))
	for _, ext := range defaults {
		set[ext] = true
	}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		switch {
		case strings.HasPrefix(p, "+"):
			ext := strings.TrimPrefix(p, "+")
			if ext != "" {
				set[ext] = true
			}
		case strings.HasPrefix(p, "-"):
			ext := strings.TrimPrefix(p, "-")
			delete(set, ext)
		default:
			if p != "" {
				set[p] = true
			}
		}
	}

	result := make([]string, 0, len(set))
	for ext := range set {
		result = append(result, ext)
	}
	sort.Strings(result)
	return result
}

// =============================================================================
// PHP Docker image tag helpers
// =============================================================================

// PHPImageTag returns the Docker image tag for the given PHP version string.
// When version is "latest" or empty, it returns the unversioned
// "php:fpm-alpine" tag; otherwise it returns "php:<version>-fpm-alpine".
func PHPImageTag(version string) string {
	if version == "" || version == constants.PHPVersionLatest {
		return constants.PHPFPMImageLatest
	}
	return fmt.Sprintf(constants.PHPFPMImageFormat, version)
}

// =============================================================================
// Nginx config generation (PHP)
// =============================================================================

// generatePHPFPMConf generates an FPM pool override file. Local-only sites
// use `pm = ondemand` so idle sites consume zero workers; production sites
// (Let's Encrypt resolver) keep the dynamic default for steady throughput.
func generatePHPFPMConf(isLocal bool) string {
	pm := "dynamic"
	if isLocal {
		pm = "ondemand"
	}
	return fmt.Sprintf(`; Generated by srv — PHP-FPM pool override.
; Mounted at %s so it sorts after the base image's www.conf.

[www]
pm = %s
pm.max_children = 16
pm.start_servers = 2
pm.min_spare_servers = 1
pm.max_spare_servers = 4
pm.process_idle_timeout = 10s
pm.max_requests = 500
`, constants.PHPFPMConfContainerPath, pm)
}

// generatePHPIni generates a php.ini with sensible development defaults.
// It is mounted into the container as a drop-in override file so it cannot
// conflict with the base image's own php.ini.
func generatePHPIni() string {
	return `; Generated by srv - PHP site configuration overrides
; This file is yours to edit. "srv site regenerate" will reset it.
; Tuned for local development — adjust values as needed.

[PHP]
memory_limit          = 2G
upload_max_filesize   = 2G
post_max_size         = 2G
max_execution_time    = 300
max_input_time        = 300
max_input_vars        = 10000
date.timezone         = UTC

; Uncomment to enable Xdebug step debugging:
;xdebug.mode          = debug
;xdebug.start_with_request = yes
;xdebug.client_host   = host.docker.internal
;xdebug.client_port   = 9003

[Session]
session.gc_maxlifetime = 86400
`
}

// generatePHPNginxConf generates an nginx configuration for a PHP site.
// limits, if non-nil, overrides default client_max_body_size and fastcgi_*_timeout
// directives. siteName + fpmHost together set the SCRIPT_FILENAME prefix and
// the upstream FPM container that lives in the shared pool.
func generatePHPNginxConf(info *PHPSiteInfo, limits *Limits, siteName, fpmHost string) string {
	// Determine the document root path inside the container. With the shared
	// FPM pool each site is mounted at /var/www/<sitename>, and both nginx
	// and FPM containers see the same path so SCRIPT_FILENAME resolves on
	// both ends.
	siteRoot := constants.PHPSiteDocRootRoot + "/" + siteName
	docRoot := siteRoot
	if info.DocumentRoot != "" {
		docRoot = siteRoot + "/" + info.DocumentRoot
	}

	// Determine entry point based on framework.
	entryPoint := "index.php"
	if info.Framework == constants.PHPFrameworkSymfony {
		// Older Symfony used app.php; check whether the web/ docroot is used
		// (a simple heuristic — if web/ then it's likely Symfony <4).
		if info.DocumentRoot == "web" {
			entryPoint = "app.php"
		}
	}

	// WordPress needs $args in its try_files.
	tryFilesArgs := "$query_string"
	if info.Framework == constants.PHPFrameworkWordPress {
		tryFilesArgs = "$args"
	}

	var sb strings.Builder
	sb.WriteString("# Generated by srv - PHP site nginx config\n")
	sb.WriteString("# This file is yours to edit. \"srv site regenerate\" will reset it.\n")
	sb.WriteString("#\n")
	sb.WriteString("# Uncomment to add a WebSocket proxy (e.g. Laravel Reverb on port 8080):\n")
	sb.WriteString("#\n")
	sb.WriteString("#   location /app {\n")
	sb.WriteString("#       proxy_pass http://php:8080;\n")
	sb.WriteString("#       proxy_http_version 1.1;\n")
	sb.WriteString("#       proxy_set_header Upgrade $http_upgrade;\n")
	sb.WriteString("#       proxy_set_header Connection \"upgrade\";\n")
	sb.WriteString("#       proxy_set_header Host $host;\n")
	sb.WriteString("#   }\n")
	sb.WriteString("\n")
	sb.WriteString("server {\n")
	sb.WriteString("    listen 80;\n")
	sb.WriteString("    server_name _;\n")
	fmt.Fprintf(&sb, "    root %s;\n", docRoot)
	fmt.Fprintf(&sb, "    index index.php index.phtml index.html;\n")
	sb.WriteString("\n")
	// Match php.ini upload_max_filesize / post_max_size so nginx doesn't
	// reject large requests before PHP even sees them.
	maxBody := "2G"
	if limits != nil && limits.MaxBody != "" {
		maxBody = limits.MaxBody
	}
	fmt.Fprintf(&sb, "    client_max_body_size %s;\n", maxBody)
	sb.WriteString("\n")

	// Gzip
	sb.WriteString("    # Gzip compression\n")
	sb.WriteString("    gzip on;\n")
	sb.WriteString("    gzip_vary on;\n")
	sb.WriteString("    gzip_min_length 1024;\n")
	sb.WriteString("    gzip_types text/plain text/css text/xml text/javascript application/javascript application/json application/xml;\n")
	sb.WriteString("\n")

	// Security headers
	sb.WriteString("    # Security headers\n")
	sb.WriteString("    add_header X-Frame-Options \"SAMEORIGIN\" always;\n")
	sb.WriteString("    add_header X-Content-Type-Options \"nosniff\" always;\n")
	sb.WriteString("    add_header X-XSS-Protection \"1; mode=block\" always;\n")
	sb.WriteString("\n")

	// Deny dotfiles (must come before the PHP location block).
	sb.WriteString("    # Block access to hidden files (dotfiles)\n")
	sb.WriteString("    location ~ /\\. {\n")
	sb.WriteString("        deny all;\n")
	sb.WriteString("        return 404;\n")
	sb.WriteString("    }\n")
	sb.WriteString("\n")

	// Laravel: also deny .env explicitly.
	if info.Framework == constants.PHPFrameworkLaravel {
		sb.WriteString("    # Block access to .env files\n")
		sb.WriteString("    location ~ \\.env$ {\n")
		sb.WriteString("        deny all;\n")
		sb.WriteString("        return 404;\n")
		sb.WriteString("    }\n")
		sb.WriteString("\n")
	}

	// Block sensitive directories.
	sb.WriteString("    # Block access to common sensitive directories\n")
	sb.WriteString("    location ~* ^/(vendor|node_modules|\\.git|\\.svn)/ {\n")
	sb.WriteString("        deny all;\n")
	sb.WriteString("        return 404;\n")
	sb.WriteString("    }\n")
	sb.WriteString("\n")

	// Main location block.
	sb.WriteString("    # Route all requests through the PHP entry point\n")
	sb.WriteString("    location / {\n")
	fmt.Fprintf(&sb, "        try_files $uri $uri/ /%s?%s;\n", entryPoint, tryFilesArgs)
	sb.WriteString("    }\n")
	sb.WriteString("\n")

	// PHP/PHTML processing via FastCGI.
	sb.WriteString("    # PHP and PHTML processing via PHP-FPM\n")
	sb.WriteString("    location ~ \\.(php|phtml)$ {\n")
	fmt.Fprintf(&sb, "        fastcgi_pass %s:%d;\n", fpmHost, constants.PHPFPMPort)
	sb.WriteString("        fastcgi_index index.php;\n")
	sb.WriteString("        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;\n")
	sb.WriteString("        include fastcgi_params;\n")
	if limits != nil {
		if limits.ConnectTimeout != "" {
			fmt.Fprintf(&sb, "        fastcgi_connect_timeout %s;\n", limits.ConnectTimeout)
		}
		if limits.SendTimeout != "" {
			fmt.Fprintf(&sb, "        fastcgi_send_timeout %s;\n", limits.SendTimeout)
		}
		if limits.ReadTimeout != "" {
			fmt.Fprintf(&sb, "        fastcgi_read_timeout %s;\n", limits.ReadTimeout)
		}
	}
	sb.WriteString("    }\n")
	sb.WriteString("}\n")

	return sb.String()
}

// =============================================================================
// Dockerfile generation (PHP)
// =============================================================================

// ipeExtensions is the full list of PHP extensions supported by
// install-php-extensions (https://github.com/mlocati/docker-php-extension-installer).
// All dependency resolution (Alpine packages, PECL vs bundled, configure flags)
// is handled automatically by IPE at build time.
var ipeExtensions = []string{
	"amqp", "apcu", "apcu_bc", "ast",
	"bcmath", "bitset", "blackfire", "brotli", "bz2",
	"calendar", "cassandra", "cmark", "csv",
	"dba", "ddtrace", "decimal", "ds",
	"ecma_intl", "enchant", "ev", "event", "excimer", "exif",
	"ffi", "ftp",
	"gd", "gearman", "geoip", "geos", "geospatial", "gettext", "gmagick", "gmp", "gnupg", "grpc",
	"http",
	"igbinary", "imagick", "imap", "inotify", "intl", "ion", "ioncube_loader", "ip2location",
	"jsmin", "json_post", "jsonpath", "judy",
	"ldap", "luasandbox", "lz4", "lzf",
	"mailparse", "maxminddb", "mbstring", "mcrypt", "md4c", "memcache", "memcached", "memprof",
	"mongodb", "msgpack", "mysqli",
	"oauth", "oci8", "odbc", "opcache", "opencensus", "openswoole", "opentelemetry", "operator",
	"parallel", "parle", "pcntl", "pcov",
	"pdo", "pdo_dblib", "pdo_firebird", "pdo_mysql", "pdo_oci", "pdo_odbc", "pdo_pgsql",
	"pdo_snowflake", "pdo_sqlite", "pdo_sqlsrv", "pgsql", "phalcon", "php_trie", "phpy",
	"pkcs11", "pq", "propro", "protobuf", "pspell", "psr",
	"raphf", "rdkafka", "redis", "relay",
	"saxon", "seasclick", "seaslog", "shmop", "simdjson", "simplexml", "smbclient", "snappy",
	"snmp", "snuffleupagus", "soap", "sockets", "sodium", "solr", "sourceguardian", "spx", "sqlsrv",
	"ssh2", "stomp", "swoole", "sync", "sysvmsg", "sysvsem", "sysvshm",
	"tensor", "tideways", "tidy", "timezonedb", "translit",
	"uopz", "uploadprogress", "uuid", "uv",
	"vips", "vld",
	"xattr", "xdebug", "xdiff", "xhprof", "xlswriter", "xmldiff", "xmlrpc", "xpass", "xsl",
	"yac", "yaml", "yar",
	"zephir_parser", "zip", "zmq", "zookeeper", "zstd",
}

// KnownPHPExtensions returns a sorted list of all PHP extensions supported by
// install-php-extensions.
func KnownPHPExtensions() []string {
	exts := make([]string, len(ipeExtensions))
	copy(exts, ipeExtensions)
	sort.Strings(exts)
	return exts
}

// builtinExtensions are always compiled into PHP — no installation step needed.
// These extensions are either statically compiled into the PHP binary or are
// deeply integrated and cannot be built as a separate shared extension via
// docker-php-ext-install (e.g. opcache, tokenizer).
var builtinExtensions = map[string]bool{
	"json":      true,
	"hash":      true,
	"openssl":   true,
	"sodium":    true,
	"filter":    true,
	"ctype":     true,
	"session":   true,
	"pcre":      true,
	"spl":       true,
	"standard":  true,
	"tokenizer": true, // requires Zend parser source files not present after PHP is compiled
	// Note: opcache is NOT listed here — IPE handles enabling it correctly via
	// docker-php-ext-enable, and we always write an opcache.ini regardless.
}

// generatePHPDockerfile generates a Dockerfile for a PHP site, installing all
// required extensions via install-php-extensions (https://github.com/mlocati/docker-php-extension-installer).
// The output is deterministic in (php_version, sorted extensions) so two sites
// with the same fingerprint produce byte-identical Dockerfiles and share the
// same `srv-php:<hash>` image. Buildkit cache mounts speed up extension
// installs by reusing /var/cache/apk across builds.
func generatePHPDockerfile(info *PHPSiteInfo) string {
	image := PHPImageTag(info.PHPVersion)

	// Filter out built-in extensions — they are already compiled into PHP.
	var exts []string
	for _, ext := range info.Extensions {
		if !builtinExtensions[ext] {
			exts = append(exts, ext)
		}
	}
	sort.Strings(exts)

	var sb strings.Builder
	sb.WriteString("# syntax=docker/dockerfile:1.6\n")
	sb.WriteString("# Generated by srv - PHP site Dockerfile\n")
	fmt.Fprintf(&sb, "FROM %s\n\n", image)

	// Install extensions and composer via install-php-extensions (IPE).
	// IPE resolves all Alpine packages, PECL vs bundled, and configure flags automatically.
	// @composer is always included so that `srv composer` works inside the container.
	sb.WriteString("# Install PHP extensions and composer\n")
	sb.WriteString("ADD --chmod=0755 https://github.com/mlocati/docker-php-extension-installer/releases/latest/download/install-php-extensions /usr/local/bin/\n")
	// Cache mounts: /var/cache/apk speeds up alpine package fetches across
	// sibling builds; /tmp/ipe-build keeps IPE's tarball cache.
	sb.WriteString("RUN --mount=type=cache,target=/var/cache/apk,sharing=locked \\\n")
	sb.WriteString("    --mount=type=cache,target=/tmp/ipe-build,sharing=locked \\\n")
	sb.WriteString("    install-php-extensions \\\n    @composer")
	for _, ext := range exts {
		sb.WriteString(" \\\n    " + ext)
	}
	sb.WriteString("\n\n")

	// Configure opcache — always write sensible defaults. opcache is included in
	// every PHP-FPM image but must be explicitly enabled via ini.
	sb.WriteString("# Configure opcache\n")
	sb.WriteString("RUN echo \"opcache.enable=1\" >> /usr/local/etc/php/conf.d/opcache.ini \\\n")
	sb.WriteString("    && echo \"opcache.enable_cli=0\" >> /usr/local/etc/php/conf.d/opcache.ini \\\n")
	sb.WriteString("    && echo \"opcache.memory_consumption=128\" >> /usr/local/etc/php/conf.d/opcache.ini \\\n")
	sb.WriteString("    && echo \"opcache.max_accelerated_files=10000\" >> /usr/local/etc/php/conf.d/opcache.ini \\\n")
	sb.WriteString("    && echo \"opcache.validate_timestamps=1\" >> /usr/local/etc/php/conf.d/opcache.ini\n\n")

	fmt.Fprintf(&sb, "WORKDIR %s\n", constants.PHPDockerRootPath)

	return sb.String()
}

// PHPImageFingerprint returns a deterministic 12-char hex tag derived from
// (php_version, sorted non-builtin extensions). Sites with the same fingerprint
// share one prebuilt image (`srv-php:<hash>`) instead of each rebuilding from
// scratch. Built-in extensions are excluded so two sites differing only in a
// no-op `--php-extensions "+pdo"` still match.
func PHPImageFingerprint(info *PHPSiteInfo) string {
	var exts []string
	for _, ext := range info.Extensions {
		if !builtinExtensions[ext] {
			exts = append(exts, ext)
		}
	}
	sort.Strings(exts)
	h := sha256.New()
	fmt.Fprintf(h, "v1|%s|%s", info.PHPVersion, strings.Join(exts, ","))
	return fmt.Sprintf("srv-php:%x", h.Sum(nil)[:6])
}

// =============================================================================
// Docker Compose generation (PHP)
// =============================================================================

// phpVolumeConfig is a bind-mount volume entry.
type phpVolumeConfig struct {
	Type        string `yaml:"type"`
	Source      string `yaml:"source"`
	Target      string `yaml:"target"`
	ReadOnly    bool   `yaml:"read_only,omitempty"`
	Consistency string `yaml:"consistency,omitempty"` // "cached" on macOS — cuts inode roundtrips
}

// phpBuildConfig is the build context for the php-fpm service.
type phpBuildConfig struct {
	Context    string `yaml:"context"`
	Dockerfile string `yaml:"dockerfile"`
}

// phpServiceConfig represents a service in the generated compose file.
type phpServiceConfig struct {
	Build         *phpBuildConfig   `yaml:"build,omitempty"`
	ContainerName string            `yaml:"container_name"`
	Image         string            `yaml:"image,omitempty"`
	PullPolicy    string            `yaml:"pull_policy,omitempty"`
	Volumes       []phpVolumeConfig `yaml:"volumes,omitempty"`
	Labels        map[string]string `yaml:"labels,omitempty"`
	Networks      []string          `yaml:"networks"`
	Restart       string            `yaml:"restart"`
	DependsOn     []string          `yaml:"depends_on,omitempty"`
	HealthCheck   *healthCheck      `yaml:"healthcheck,omitempty"`
}

// healthCheck is a compose-format healthcheck spec. Kept generic so every
// site type can emit one with the same shape.
type healthCheck struct {
	Test        []string `yaml:"test"`
	Interval    string   `yaml:"interval,omitempty"`
	Timeout     string   `yaml:"timeout,omitempty"`
	StartPeriod string   `yaml:"start_period,omitempty"`
	Retries     int      `yaml:"retries,omitempty"`
}

// makeHealthCheck builds a cheap TCP-probe healthcheck for the given port.
// Uses busybox `nc -z` which is present in alpine, debian-slim, ubuntu,
// nginx:alpine, php:*-fpm-alpine, node:*-alpine, ruby:*-alpine, and python:*-alpine.
// Falls back gracefully on images without nc — the check just marks unhealthy.
func makeHealthCheck(port int) *healthCheck {
	return &healthCheck{
		Test:        []string{"CMD-SHELL", fmt.Sprintf("nc -z 127.0.0.1 %d || exit 1", port)},
		Interval:    "30s",
		Timeout:     "3s",
		StartPeriod: "5s",
		Retries:     3,
	}
}

// phpNetworkConfig is a Docker network entry.
type phpNetworkConfig struct {
	Name     string `yaml:"name"`
	External bool   `yaml:"external"`
}

// phpComposeConfig is the top-level generated docker-compose structure.
// `name` groups every srv-managed compose project under the same umbrella so
// `docker compose ls` aggregates them in one row.
type phpComposeConfig struct {
	Name     string                      `yaml:"name,omitempty"`
	Services map[string]phpServiceConfig `yaml:"services"`
	Networks map[string]phpNetworkConfig `yaml:"networks"`
}

// WritePHPSiteConfig generates and writes the nginx.conf and docker-compose.yml
// for a PHP site into the srv config directory, AND ensures the shared FPM pool
// for this site's (php_version, extensions) fingerprint exists with this site
// listed as a member.
//
// Dockerfile / php.ini / php-fpm.conf are NOT written into the per-site dir
// any more; they live with the pool. The per-site compose declares only the
// nginx web container, which talks to the pool's FPM container by name.
//
// If force is false, existing files are left untouched so user edits are preserved.
func WritePHPSiteConfig(name string, meta SiteMetadata, info *PHPSiteInfo, force bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	siteDir := SiteConfigDir(cfg, name)
	if err := os.MkdirAll(siteDir, constants.DirPermDefault); err != nil {
		return fmt.Errorf("failed to create site config directory: %w", err)
	}

	// Ensure the shared FPM pool is configured before generating the site's
	// nginx.conf — we need the pool's container name for fastcgi_pass.
	fpmContainer, err := ensurePoolForSite(cfg, name, meta, info)
	if err != nil {
		return fmt.Errorf("ensure FPM pool: %w", err)
	}

	// Write nginx.conf.
	nginxConf := generatePHPNginxConf(info, meta.Limits, name, fpmContainer)
	nginxConfPath := SiteNginxConfPath(cfg, name)
	if err := writeFile(nginxConfPath, []byte(nginxConf), force); err != nil {
		return fmt.Errorf("failed to write nginx.conf: %w", err)
	}

	// Build site docker-compose.yml — only the nginx web container.
	webContainerName := "srv-" + name + "-web"
	labels := buildStaticTraefikLabels(name, meta.Domains, meta.IsLocal, meta.Wildcard)
	if HasListener(meta.Listeners, constants.ListenerInternal) {
		addInternalListenerLabels(labels, name, meta.Domains, meta.Wildcard)
	}

	siteMount := constants.PHPSiteDocRootRoot + "/" + name
	composeConfig := phpComposeConfig{
		Name: constants.ComposeProjectName,
		Services: map[string]phpServiceConfig{
			constants.PHPWebServiceName: {
				ContainerName: webContainerName,
				Image:         constants.ImageNginxAlpine,
				Volumes: []phpVolumeConfig{
					{
						Type:        "bind",
						Source:      meta.ProjectPath,
						Target:      siteMount,
						ReadOnly:    true,
						Consistency: volumeConsistencyForHost(),
					},
					{
						Type:     "bind",
						Source:   nginxConfPath,
						Target:   constants.NginxDefaultConfPath,
						ReadOnly: true,
					},
				},
				Labels:      labels,
				Networks:    []string{constants.TraefikSubdir},
				Restart:     constants.RestartUnlessStopped,
				HealthCheck: makeHealthCheck(80),
			},
		},
		Networks: map[string]phpNetworkConfig{
			constants.TraefikSubdir: {
				Name:     meta.NetworkName,
				External: true,
			},
		},
	}

	data, err := yaml.Marshal(&composeConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal compose config: %w", err)
	}

	header := fmt.Sprintf("# Generated by srv - PHP site (%s)\n# Project: %s\n#\n# FPM lives in the shared pool container %s.\n# This file is yours to edit. Changes take effect on next restart.\n\n",
		info.Framework, meta.ProjectPath, fpmContainer)
	content := header + string(data)

	composePath := SiteComposePath(cfg, name)
	return writeFile(composePath, []byte(content), force)
}

// ensurePoolForSite resolves the FPM pool fingerprint for this site, writes
// (or refreshes) the pool's compose file with every PHP site that shares the
// fingerprint, starts the pool container, and returns the pool container name.
// Callers must invoke this before generating the site's nginx.conf because
// fastcgi_pass needs the pool container name.
func ensurePoolForSite(cfg *config.Config, siteName string, meta SiteMetadata, info *PHPSiteInfo) (string, error) {
	exts := nonBuiltinExtensions(info.Extensions)
	fp := pool.Fingerprint(info.PHPVersion, exts)

	// Collect every other PHP site that belongs to the same pool.
	members, err := collectPoolMembers(fp, siteName, meta.ProjectPath)
	if err != nil {
		return "", err
	}

	spec := pool.Spec{
		PHPVersion: info.PHPVersion,
		Extensions: exts,
		Members:    members,
	}

	dockerfile := generatePHPDockerfile(info)
	phpIni := generatePHPIni()
	fpmConf := generatePHPFPMConf(meta.IsLocal)

	if err := pool.WriteFiles(cfg, spec, dockerfile, phpIni, fpmConf); err != nil {
		return "", err
	}
	if err := docker.ComposeUp(pool.Dir(cfg, fp)); err != nil {
		return "", fmt.Errorf("start FPM pool: %w", err)
	}
	return pool.ContainerName(fp), nil
}

// collectPoolMembers builds the full member list for a pool: this site plus
// every other PHP site whose metadata yields the same fingerprint.
func collectPoolMembers(fp, siteName, projectPath string) ([]pool.Member, error) {
	members := []pool.Member{{SiteName: siteName, ProjectPath: projectPath}}

	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(cfg.SitesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return members, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == siteName || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		other, err := ReadSiteMetadata(entry.Name())
		if err != nil || other == nil || other.Type != SiteTypePHP {
			continue
		}
		// Reconstruct the fingerprint from the other site's metadata. Reuse
		// the same hash function so it matches even after extensions reorder.
		otherInfo := &PHPSiteInfo{
			PHPVersion: other.PHPVersion,
			Extensions: other.PHPExtensions,
		}
		otherFP := pool.Fingerprint(otherInfo.PHPVersion, nonBuiltinExtensions(otherInfo.Extensions))
		if otherFP != fp {
			continue
		}
		members = append(members, pool.Member{SiteName: entry.Name(), ProjectPath: other.ProjectPath})
	}
	return members, nil
}

// RemoveSiteFromPool removes a PHP site from its FPM pool and regenerates the
// pool's compose file. If the pool's member set becomes empty, the pool's
// containers are torn down and its directory is deleted. Called from the
// `srv remove` flow before site metadata is wiped.
func RemoveSiteFromPool(siteName string) error {
	meta, err := ReadSiteMetadata(siteName)
	if err != nil || meta == nil || meta.Type != SiteTypePHP {
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	info := &PHPSiteInfo{PHPVersion: meta.PHPVersion, Extensions: meta.PHPExtensions}
	exts := nonBuiltinExtensions(info.Extensions)
	fp := pool.Fingerprint(info.PHPVersion, exts)

	// Members minus this site.
	members, err := collectPoolMembers(fp, siteName, meta.ProjectPath)
	if err != nil {
		return err
	}
	filtered := members[:0]
	for _, m := range members {
		if m.SiteName == siteName {
			continue
		}
		filtered = append(filtered, m)
	}

	// Empty pool → tear it down completely.
	if len(filtered) == 0 {
		_ = docker.ComposeDown(pool.Dir(cfg, fp))
		return pool.Remove(cfg, fp)
	}

	// Otherwise rewrite the pool compose without this site and recreate.
	spec := pool.Spec{
		PHPVersion: info.PHPVersion,
		Extensions: exts,
		Members:    filtered,
	}
	if err := pool.WriteFiles(cfg, spec, generatePHPDockerfile(info), generatePHPIni(), generatePHPFPMConf(meta.IsLocal)); err != nil {
		return err
	}
	return docker.ComposeUp(pool.Dir(cfg, fp))
}

// IsBuiltinPHPExtension reports whether ext ships pre-compiled into the base
// php:*-fpm-alpine image (and therefore does not contribute to the pool
// fingerprint).
func IsBuiltinPHPExtension(ext string) bool {
	return builtinExtensions[ext]
}

// nonBuiltinExtensions returns the input list filtered to extensions that
// install-php-extensions actually needs to install (built-ins are part of
// the base image).
func nonBuiltinExtensions(exts []string) []string {
	out := make([]string, 0, len(exts))
	for _, e := range exts {
		if !builtinExtensions[e] {
			out = append(out, e)
		}
	}
	return out
}

// WritePHPDockerConfig regenerates the pool's Dockerfile + the site's
// docker-compose.yml after a PHP version or extension change. User-editable
// files (php.ini, nginx.conf) are left untouched.
func WritePHPDockerConfig(name string, meta SiteMetadata, info *PHPSiteInfo) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	nginxConfPath := SiteNginxConfPath(cfg, name)

	// Refresh the pool (the Dockerfile now lives there) for the new fingerprint.
	fpmContainer, err := ensurePoolForSite(cfg, name, meta, info)
	if err != nil {
		return fmt.Errorf("ensure FPM pool: %w", err)
	}

	webContainerName := "srv-" + name + "-web"

	labels := buildStaticTraefikLabels(name, meta.Domains, meta.IsLocal, meta.Wildcard)
	if HasListener(meta.Listeners, constants.ListenerInternal) {
		addInternalListenerLabels(labels, name, meta.Domains, meta.Wildcard)
	}

	siteMount := constants.PHPSiteDocRootRoot + "/" + name
	composeConfig := phpComposeConfig{
		Name: constants.ComposeProjectName,
		Services: map[string]phpServiceConfig{
			constants.PHPWebServiceName: {
				ContainerName: webContainerName,
				Image:         constants.ImageNginxAlpine,
				Volumes: []phpVolumeConfig{
					{Type: "bind", Source: meta.ProjectPath, Target: siteMount, ReadOnly: true, Consistency: volumeConsistencyForHost()},
					{Type: "bind", Source: nginxConfPath, Target: constants.NginxDefaultConfPath, ReadOnly: true},
				},
				Labels:      labels,
				Networks:    []string{constants.TraefikSubdir},
				Restart:     constants.RestartUnlessStopped,
				HealthCheck: makeHealthCheck(80),
			},
		},
		Networks: map[string]phpNetworkConfig{
			constants.TraefikSubdir: {Name: meta.NetworkName, External: true},
		},
	}

	data, err := yaml.Marshal(&composeConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal compose config: %w", err)
	}

	header := fmt.Sprintf("# Generated by srv - PHP site (%s)\n# Project: %s\n#\n# FPM lives in the shared pool container %s.\n# This file is yours to edit. Changes take effect on next restart.\n\n",
		info.Framework, meta.ProjectPath, fpmContainer)
	content := header + string(data)

	composePath := SiteComposePath(cfg, name)
	return os.WriteFile(composePath, []byte(content), constants.FilePermDefault)
}

// =============================================================================
// Small filesystem helpers
// =============================================================================

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func hasComposerPackagePrefix(composer *ComposerJSON, prefix string) bool {
	for key := range composer.Require {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}
