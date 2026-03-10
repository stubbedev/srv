// Package site handles site management operations.
package site

import (
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
	extensions := ExtractComposerExtensions(&composer)

	return &PHPSiteInfo{
		PHPVersion:   phpVersion,
		Extensions:   extensions,
		Framework:    framework,
		DocumentRoot: docRoot,
	}, nil
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

// RawPHPDefaults returns PHPSiteInfo with the extensive default extension set
// and the latest PHP image tag, used when no composer.json is available.
func RawPHPDefaults() *PHPSiteInfo {
	return &PHPSiteInfo{
		PHPVersion:   constants.PHPVersionLatest,
		Extensions:   DefaultPHPExtensions(),
		Framework:    constants.PHPFrameworkGeneric,
		DocumentRoot: "",
	}
}

// DefaultPHPExtensions returns an extensive default set of PHP extensions for
// projects that have no composer.json to declare their requirements.
func DefaultPHPExtensions() []string {
	return []string{
		// Database
		"pdo",
		"pdo_mysql",
		"pdo_pgsql",
		"pdo_sqlite",
		"mysqli",
		"pgsql",
		// mongodb is installed via PECL (handled separately in Dockerfile generation)
		"mongodb",
		// Core functionality
		"mbstring",
		"xml",
		"curl",
		"tokenizer",
		// File handling
		"zip",
		"fileinfo",
		// Image processing
		"gd",
		"exif",
		// Text processing
		"iconv",
		"simplexml",
		"dom",
		// Performance & math
		"opcache",
		"bcmath",
		// Internationalization
		"intl",
		"gettext",
		// Other common needs
		"sockets",
		"ftp",
		"calendar",
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
		if strings.HasPrefix(key, "ext-") {
			exts = append(exts, strings.TrimPrefix(key, "ext-"))
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

// generatePHPNginxConf generates an nginx configuration for a PHP site.
func generatePHPNginxConf(info *PHPSiteInfo) string {
	// Determine the document root path inside the container.
	docRoot := constants.PHPDockerRootPath
	if info.DocumentRoot != "" {
		docRoot = constants.PHPDockerRootPath + "/" + info.DocumentRoot
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
	sb.WriteString("server {\n")
	sb.WriteString("    listen 80;\n")
	sb.WriteString("    server_name _;\n")
	sb.WriteString(fmt.Sprintf("    root %s;\n", docRoot))
	sb.WriteString(fmt.Sprintf("    index index.php index.phtml index.html;\n"))
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
	sb.WriteString(fmt.Sprintf("        try_files $uri $uri/ /%s?%s;\n", entryPoint, tryFilesArgs))
	sb.WriteString("    }\n")
	sb.WriteString("\n")

	// PHP/PHTML processing via FastCGI.
	sb.WriteString("    # PHP and PHTML processing via PHP-FPM\n")
	sb.WriteString("    location ~ \\.(php|phtml)$ {\n")
	sb.WriteString(fmt.Sprintf("        fastcgi_pass %s:%d;\n", constants.PHPFPMServiceName, constants.PHPFPMPort))
	sb.WriteString("        fastcgi_index index.php;\n")
	sb.WriteString("        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;\n")
	sb.WriteString("        include fastcgi_params;\n")
	sb.WriteString("    }\n")
	sb.WriteString("}\n")

	return sb.String()
}

// =============================================================================
// Dockerfile generation (PHP)
// =============================================================================

// extensionDeps maps a PHP extension name to the Alpine system packages it
// requires, and whether it needs a special configure or PECL install step.
type extInfo struct {
	alpinePkgs []string // apk packages required
	configure  string   // extra configure flags (empty if none)
	pecl       bool     // true if installed via PECL instead of docker-php-ext-install
}

// phpExtensionCatalog is the catalogue of all known extensions and their
// build requirements on Alpine Linux.
var phpExtensionCatalog = map[string]extInfo{
	// Image processing
	"gd":   {alpinePkgs: []string{"libpng-dev", "libjpeg-turbo-dev", "freetype-dev"}, configure: "--with-freetype --with-jpeg"},
	"exif": {},
	// Database
	"pdo":        {},
	"pdo_mysql":  {},
	"pdo_pgsql":  {alpinePkgs: []string{"postgresql-dev"}},
	"pdo_sqlite": {alpinePkgs: []string{"sqlite-dev"}},
	"mysqli":     {},
	"pgsql":      {alpinePkgs: []string{"postgresql-dev"}},
	"mongodb":    {alpinePkgs: []string{"autoconf", "g++", "make"}, pecl: true},
	// Core
	"mbstring":  {},
	"xml":       {alpinePkgs: []string{"libxml2-dev"}},
	"curl":      {alpinePkgs: []string{"curl-dev"}},
	"tokenizer": {},
	// File handling
	"zip":      {alpinePkgs: []string{"libzip-dev"}},
	"fileinfo": {},
	// Text processing
	"iconv":     {},
	"simplexml": {alpinePkgs: []string{"libxml2-dev"}},
	"dom":       {alpinePkgs: []string{"libxml2-dev"}},
	// Performance & math
	"opcache": {},
	"bcmath":  {},
	// Internationalization
	"intl":    {alpinePkgs: []string{"icu-dev"}},
	"gettext": {alpinePkgs: []string{"gettext-dev"}},
	// Networking
	"sockets":  {},
	"ftp":      {},
	"calendar": {},
	// Security (usually built-in, but listed for completeness)
	"openssl": {},
	"sodium":  {},
	"hash":    {},
}

// builtinExtensions are always compiled into PHP — no installation step needed.
var builtinExtensions = map[string]bool{
	"json":     true,
	"hash":     true,
	"openssl":  true,
	"sodium":   true,
	"filter":   true,
	"ctype":    true,
	"session":  true,
	"pcre":     true,
	"spl":      true,
	"standard": true,
}

// generatePHPDockerfile generates a Dockerfile for a PHP site, installing all
// required extensions.
func generatePHPDockerfile(info *PHPSiteInfo) string {
	image := PHPImageTag(info.PHPVersion)

	// Partition extensions into categories.
	var standardExts []string // docker-php-ext-install
	var peclExts []string     // pecl install
	var configureExts []string
	apkPkgs := make(map[string]bool)
	hasPeclExts := false

	for _, ext := range info.Extensions {
		if builtinExtensions[ext] {
			continue
		}
		ei, known := phpExtensionCatalog[ext]
		if !known {
			// Unknown extension: attempt docker-php-ext-install and hope for the best.
			standardExts = append(standardExts, ext)
			continue
		}
		for _, pkg := range ei.alpinePkgs {
			apkPkgs[pkg] = true
		}
		if ei.pecl {
			peclExts = append(peclExts, ext)
			hasPeclExts = true
		} else {
			if ei.configure != "" {
				configureExts = append(configureExts, ext)
			}
			standardExts = append(standardExts, ext)
		}
	}

	// PECL builds always need these build-time tools.
	if hasPeclExts {
		apkPkgs["autoconf"] = true
		apkPkgs["g++"] = true
		apkPkgs["make"] = true
	}

	sort.Strings(standardExts)
	sort.Strings(peclExts)
	sort.Strings(configureExts)
	pkgList := sortedKeys(apkPkgs)

	var sb strings.Builder
	sb.WriteString("# Generated by srv - PHP site Dockerfile\n")
	sb.WriteString(fmt.Sprintf("FROM %s\n", image))
	sb.WriteString("\n")

	// System dependencies.
	if len(pkgList) > 0 {
		sb.WriteString("# Install system dependencies\n")
		sb.WriteString("RUN apk add --no-cache")
		for _, pkg := range pkgList {
			sb.WriteString(" \\\n    " + pkg)
		}
		sb.WriteString("\n\n")
	}

	// Configure steps (e.g. GD with JPEG/FreeType).
	if len(configureExts) > 0 {
		for _, ext := range configureExts {
			ei := phpExtensionCatalog[ext]
			sb.WriteString(fmt.Sprintf("RUN docker-php-ext-configure %s %s\n", ext, ei.configure))
		}
		sb.WriteString("\n")
	}

	// Standard extensions.
	if len(standardExts) > 0 {
		// Remove configure-only exts that are also in standardExts (they need both).
		sb.WriteString("# Install PHP extensions\n")
		sb.WriteString("RUN docker-php-ext-install -j$(nproc)")
		for _, ext := range standardExts {
			sb.WriteString(" \\\n    " + ext)
		}
		sb.WriteString("\n\n")
	}

	// PECL extensions.
	for _, ext := range peclExts {
		sb.WriteString(fmt.Sprintf("# Install %s via PECL\n", ext))
		sb.WriteString(fmt.Sprintf("RUN pecl install %s \\\n    && docker-php-ext-enable %s\n\n", ext, ext))
	}

	// Clean up build-time tools if they were only needed for PECL.
	if hasPeclExts {
		sb.WriteString("# Remove build-time tools\n")
		sb.WriteString("RUN apk del autoconf g++ make\n\n")
	}

	// Configure opcache if requested.
	hasOpcache := false
	for _, ext := range info.Extensions {
		if ext == "opcache" {
			hasOpcache = true
			break
		}
	}
	if hasOpcache {
		sb.WriteString("# Configure opcache\n")
		sb.WriteString("RUN echo \"opcache.enable=1\" >> /usr/local/etc/php/conf.d/opcache.ini \\\n")
		sb.WriteString("    && echo \"opcache.memory_consumption=128\" >> /usr/local/etc/php/conf.d/opcache.ini \\\n")
		sb.WriteString("    && echo \"opcache.max_accelerated_files=10000\" >> /usr/local/etc/php/conf.d/opcache.ini\n\n")
	}

	sb.WriteString(fmt.Sprintf("WORKDIR %s\n", constants.PHPDockerRootPath))

	return sb.String()
}

// sortedKeys returns a sorted slice of the keys of a map[string]bool.
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// =============================================================================
// Docker Compose generation (PHP)
// =============================================================================

// phpVolumeConfig is a bind-mount volume entry.
type phpVolumeConfig struct {
	Type     string `yaml:"type"`
	Source   string `yaml:"source"`
	Target   string `yaml:"target"`
	ReadOnly bool   `yaml:"read_only,omitempty"`
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
	Volumes       []phpVolumeConfig `yaml:"volumes,omitempty"`
	Labels        map[string]string `yaml:"labels,omitempty"`
	Networks      []string          `yaml:"networks"`
	Restart       string            `yaml:"restart"`
	DependsOn     []string          `yaml:"depends_on,omitempty"`
}

// phpNetworkConfig is a Docker network entry.
type phpNetworkConfig struct {
	Name     string `yaml:"name"`
	External bool   `yaml:"external"`
}

// phpComposeConfig is the top-level generated docker-compose structure.
type phpComposeConfig struct {
	Services map[string]phpServiceConfig `yaml:"services"`
	Networks map[string]phpNetworkConfig `yaml:"networks"`
}

// WritePHPSiteConfig generates and writes the Dockerfile, nginx.conf, and
// docker-compose.yml for a PHP site into the srv config directory.
func WritePHPSiteConfig(name string, meta SiteMetadata, info *PHPSiteInfo) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	siteDir := SiteConfigDir(cfg, name)
	if err := os.MkdirAll(siteDir, constants.DirPermDefault); err != nil {
		return fmt.Errorf("failed to create site config directory: %w", err)
	}

	// Write Dockerfile.
	dockerfile := generatePHPDockerfile(info)
	dockerfilePath := filepath.Join(siteDir, constants.PHPDockerfileFile)
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), constants.FilePermDefault); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}

	// Write nginx.conf.
	nginxConf := generatePHPNginxConf(info)
	nginxConfPath := SiteNginxConfPath(cfg, name)
	if err := os.WriteFile(nginxConfPath, []byte(nginxConf), constants.FilePermDefault); err != nil {
		return fmt.Errorf("failed to write nginx.conf: %w", err)
	}

	// Build docker-compose.yml.
	phpContainerName := "srv-" + name + "-php"
	webContainerName := "srv-" + name + "-web"
	internalNetworkName := "srv-" + name + "-internal"

	labels := buildStaticTraefikLabels(name, meta.Domain, meta.IsLocal)

	// Determine the nginx document root mount target.
	// The project is always mounted to /var/www/html; nginx's root directive
	// inside the config already appends the sub-directory when needed.
	composeConfig := phpComposeConfig{
		Services: map[string]phpServiceConfig{
			constants.PHPFPMServiceName: {
				Build: &phpBuildConfig{
					Context:    siteDir,
					Dockerfile: constants.PHPDockerfileFile,
				},
				ContainerName: phpContainerName,
				Volumes: []phpVolumeConfig{
					{
						Type:   "bind",
						Source: meta.ProjectPath,
						Target: constants.PHPDockerRootPath,
					},
				},
				Networks: []string{"internal"},
				Restart:  constants.RestartUnlessStopped,
			},
			constants.PHPWebServiceName: {
				ContainerName: webContainerName,
				Image:         constants.ImageNginxAlpine,
				Volumes: []phpVolumeConfig{
					{
						Type:     "bind",
						Source:   meta.ProjectPath,
						Target:   constants.PHPDockerRootPath,
						ReadOnly: true,
					},
					{
						Type:     "bind",
						Source:   nginxConfPath,
						Target:   constants.NginxDefaultConfPath,
						ReadOnly: true,
					},
				},
				Labels:    labels,
				Networks:  []string{"internal", constants.TraefikSubdir},
				Restart:   constants.RestartUnlessStopped,
				DependsOn: []string{constants.PHPFPMServiceName},
			},
		},
		Networks: map[string]phpNetworkConfig{
			"internal": {
				Name: internalNetworkName,
			},
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

	header := fmt.Sprintf("# Generated by srv - PHP site (%s)\n# Project: %s\n# Do not edit - changes will be overwritten\n\n",
		info.Framework, meta.ProjectPath)
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
