// Package site handles site management operations.
package site

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/traefik"
)

// Site represents a registered site.
type Site struct {
	Name               string   // Name of the site (directory name in sites/)
	Dir                string   // Resolved directory path (project directory)
	Domains            []string // All hostnames; Domains[0] is canonical
	IsLocal            bool     // Whether it uses local SSL
	Wildcard           bool     // Match apex + one-level subdomains
	Type               SiteType // compose or static
	IsBroken           bool     // Whether the project directory exists
	Status             string   // Container status
	ServiceName        string   // Container name (for Traefik routing)
	ComposeServiceName string   // Docker Compose service name (for compose commands)
	Profile            string   // Docker Compose profile (if service uses profiles)
	Port               int      // Port (for compose sites)
	ComposeDir         string   // Directory containing docker-compose.yml (may differ from Dir for static sites)
}

// Domain returns the canonical (first) hostname for the site, or "" if none.
func (s *Site) Domain() string {
	if s == nil || len(s.Domains) == 0 {
		return ""
	}
	return s.Domains[0]
}

// ComposeFile represents a docker-compose.yml structure.
type ComposeFile struct {
	Services map[string]ComposeService `yaml:"services"`
}

// ComposeService represents a service in docker-compose.
type ComposeService struct {
	Image         string            `yaml:"image"`
	ContainerName string            `yaml:"container_name"`
	Labels        ComposeLabels     `yaml:"labels"`
	Profiles      []string          `yaml:"profiles"`
	Ports         []string          `yaml:"ports"`       // e.g., ["8080:80", "443:443"]
	Expose        []string          `yaml:"expose"`      // e.g., ["80", "3000"]
	Environment   ComposeEnvVars    `yaml:"environment"` // Environment variables
	EnvFile       ComposeStringList `yaml:"env_file"`    // .env file paths
}

// ComposeLabels handles both array and map label formats.
type ComposeLabels []string

// UnmarshalYAML implements custom unmarshaling for labels.
func (l *ComposeLabels) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.SequenceNode:
		var labels []string
		if err := value.Decode(&labels); err != nil {
			return err
		}
		*l = labels
	case yaml.MappingNode:
		var labels map[string]string
		if err := value.Decode(&labels); err != nil {
			return err
		}
		result := make([]string, 0, len(labels))
		for k, v := range labels {
			result = append(result, fmt.Sprintf("%s=%s", k, v))
		}
		*l = result
	default:
		*l = nil
	}
	return nil
}

// ComposeEnvVars handles both array and map environment formats in docker-compose.
type ComposeEnvVars map[string]string

// UnmarshalYAML implements custom unmarshaling for environment variables.
func (e *ComposeEnvVars) UnmarshalYAML(value *yaml.Node) error {
	*e = make(map[string]string)
	switch value.Kind {
	case yaml.SequenceNode:
		// Array format: ["KEY=value", "KEY2=value2"]
		var envList []string
		if err := value.Decode(&envList); err != nil {
			return err
		}
		for _, env := range envList {
			if idx := strings.Index(env, "="); idx > 0 {
				(*e)[env[:idx]] = env[idx+1:]
			}
		}
	case yaml.MappingNode:
		// Map format: KEY: value
		var envMap map[string]string
		if err := value.Decode(&envMap); err != nil {
			return err
		}
		*e = envMap
	}
	return nil
}

// ComposeStringList handles both single string and array formats (for env_file).
type ComposeStringList []string

// UnmarshalYAML implements custom unmarshaling for string or string array.
func (s *ComposeStringList) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		// Single string: env_file: .env
		var single string
		if err := value.Decode(&single); err != nil {
			return err
		}
		*s = []string{single}
	case yaml.SequenceNode:
		// Array: env_file: [.env, .env.local]
		var list []string
		if err := value.Decode(&list); err != nil {
			return err
		}
		*s = list
	default:
		*s = nil
	}
	return nil
}

// siteStatusResult holds the result of a container status check.
type siteStatusResult struct {
	index  int
	status string
}

// loadSiteFromDir loads site information from a site config directory.
// Returns the site and whether it needs a status check.
func loadSiteFromDir(cfg *config.Config, entry os.DirEntry) (Site, bool) {
	s := Site{
		Name: entry.Name(),
	}

	// Read site metadata
	meta, err := ReadSiteMetadata(entry.Name())
	if err != nil || meta == nil {
		// No valid metadata - skip this directory
		s.IsBroken = true
		return s, false
	}

	s.Domains = append([]string(nil), meta.Domains...)
	s.IsLocal = meta.IsLocal
	s.Wildcard = meta.Wildcard
	s.Type = meta.Type
	s.ServiceName = meta.ServiceName
	s.ComposeServiceName = meta.ComposeServiceName
	s.Profile = meta.Profile
	s.Port = meta.Port
	s.Dir = meta.ProjectPath

	// Fallback: if ComposeServiceName is empty, use ServiceName (backward compatibility)
	if s.ComposeServiceName == "" && s.ServiceName != "" {
		s.ComposeServiceName = s.ServiceName
	}

	// Check if project path exists
	if _, err := os.Stat(meta.ProjectPath); err != nil {
		s.IsBroken = true
		return s, false
	}

	// Determine compose directory based on site type
	switch meta.Type {
	case SiteTypeStatic, SiteTypePHP, SiteTypeNode, SiteTypeRuby, SiteTypePython, SiteTypeDockerfile:
		// srv-managed sites have their compose file in the srv config dir
		s.ComposeDir = SiteConfigDir(cfg, entry.Name())
	default:
		// Compose sites use the project directory
		s.ComposeDir = meta.ProjectPath
	}

	return s, true // Needs status check
}

// siteContainerStatus returns the container status for a site using the most
// efficient available method:
//   - Static/PHP sites have a single container with a known name → Docker SDK inspect (no subprocess).
//   - Compose sites have their containers associated with a working-dir label → Docker SDK list (no subprocess).
//   - Fallback (no service name, unknown type): subprocess docker compose ps.
func siteContainerStatus(s Site) string {
	switch s.Type {
	case SiteTypeStatic, SiteTypePHP, SiteTypeNode, SiteTypeRuby, SiteTypePython, SiteTypeDockerfile:
		// Single-container sites: service name IS the container name.
		if s.ServiceName != "" {
			return docker.ContainerStatusByName(s.ServiceName)
		}
	case SiteTypeCompose:
		// Multi-container compose projects: query by working-dir label.
		if s.ComposeDir != "" {
			return docker.ContainerStatusByComposeDir(s.ComposeDir)
		}
	}
	// Fallback: subprocess docker compose ps (backward compat).
	return docker.ContainerStatus(s.ComposeDir)
}

// fetchSiteStatuses fetches container statuses for sites in parallel.
func fetchSiteStatuses(sites []Site, indices []int) {
	if len(indices) == 0 {
		return
	}

	workers := min(constants.MaxStatusWorkers, len(indices))

	var wg sync.WaitGroup
	statusChan := make(chan siteStatusResult, len(indices))

	// Semaphore for limiting concurrency
	sem := make(chan struct{}, workers)

	for _, idx := range indices {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Recover from any panic so wg.Done() is always called and
			// the collector goroutine is never left blocking forever.
			defer func() {
				if r := recover(); r != nil {
					statusChan <- siteStatusResult{i, "unknown"}
				}
			}()
			sem <- struct{}{}        // Acquire
			defer func() { <-sem }() // Release

			status := siteContainerStatus(sites[i])
			statusChan <- siteStatusResult{i, status}
		}(idx)
	}

	// Wait for all goroutines to complete
	go func() {
		wg.Wait()
		close(statusChan)
	}()

	// Collect results
	for result := range statusChan {
		sites[result.index].Status = result.status
	}
}

// List returns all registered sites.
// Container status checks are done in parallel for better performance.
func List() ([]Site, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(cfg.SitesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Site{}, nil
		}
		return nil, err
	}

	// First pass: collect site metadata (fast, sequential)
	var sites []Site
	var validSiteIndices []int // Indices of sites that need status check

	for _, entry := range entries {
		// Only process directories (site config dirs).
		// Skip internal directories prefixed with "_" (e.g. _proxy-* cert dirs).
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}

		site, needsStatus := loadSiteFromDir(cfg, entry)
		if needsStatus {
			validSiteIndices = append(validSiteIndices, len(sites))
		}
		sites = append(sites, site)
	}

	// Second pass: fetch container status in parallel
	fetchSiteStatuses(sites, validSiteIndices)

	return sites, nil
}

// Get returns a specific site by name.
// It loads all registered sites and therefore performs a parallel status check
// for every site. Prefer GetByName when you only need a single site.
func Get(name string) (*Site, error) {
	sites, err := List()
	if err != nil {
		return nil, err
	}

	for _, s := range sites {
		if s.Name == name {
			return &s, nil
		}
	}

	return nil, fmt.Errorf("site not found: %s", name)
}

// GetByName returns a single site by name without loading all registered sites.
// It reads only that site's metadata and fetches its container status directly,
// making it significantly faster than Get when the full list is not needed.
func GetByName(name string) (*Site, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	// Verify the site config directory exists.
	siteDir := SiteConfigDir(cfg, name)
	if _, err := os.Stat(siteDir); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("site not found: %s", name)
		}
		return nil, err
	}

	// Synthesise a DirEntry-like object using the site name.
	entries, err := os.ReadDir(filepath.Dir(siteDir))
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.Name() == name && entry.IsDir() {
			s, needsStatus := loadSiteFromDir(cfg, entry)
			if needsStatus {
				s.Status = siteContainerStatus(s)
			}
			return &s, nil
		}
	}

	return nil, fmt.Errorf("site not found: %s", name)
}

// composeNotFoundError is returned by FindComposeFile when no compose file exists.
type composeNotFoundError struct {
	dir string
}

func (e *composeNotFoundError) Error() string {
	return fmt.Sprintf("no docker-compose file found in %s", e.dir)
}

// IsNotFoundError reports whether err is a "compose file not found" error as
// returned by FindComposeFile. It returns false for real I/O errors such as
// permission denied.
func IsNotFoundError(err error) bool {
	var nfe *composeNotFoundError
	return errors.As(err, &nfe)
}

// FindComposeFile finds the docker-compose file in a directory.
// If no compose file exists it returns an *composeNotFoundError; use
// IsNotFoundError to distinguish this from other I/O errors.
func FindComposeFile(dir string) (string, error) {
	candidates := []string{
		"docker-compose.yml",
		"docker-compose.yaml",
		"compose.yml",
		"compose.yaml",
	}

	for _, name := range candidates {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		} else if !os.IsNotExist(err) {
			// Real I/O error (e.g. permission denied) — surface it immediately.
			return "", fmt.Errorf("checking for compose file %s: %w", path, err)
		}
	}

	return "", &composeNotFoundError{dir: dir}
}

// ParseComposeFile parses a docker-compose.yml file.
func ParseComposeFile(path string) (*ComposeFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var compose ComposeFile
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return nil, err
	}

	return &compose, nil
}

// ServiceInfo holds information about a compose service for selection.
type ServiceInfo struct {
	ServiceName   string   // The service name in docker-compose
	ContainerName string   // The container_name (or derived name if not set)
	Profiles      []string // The profiles this service belongs to (empty = always runs)
	Port          int      // Discovered container port (0 if not found)
}

// GetServiceInfos returns service information from a compose file.
// For each service, it returns the container name that Traefik should route to.
func GetServiceInfos(composePath string) ([]ServiceInfo, error) {
	compose, err := ParseComposeFile(composePath)
	if err != nil {
		return nil, err
	}

	// Get the project name (directory name) for deriving container names.
	// Docker Compose v2 uses the directory name lowercased with hyphens kept
	// as-is: e.g. "my-app" → container "my-app-web-1".
	// (Docker Compose v1 used underscores but v1 is EOL.)
	projectDir := filepath.Dir(composePath)
	projectName := strings.ToLower(filepath.Base(projectDir))

	// Load environment variables from env files and environment
	envVars := loadEnvVarsForCompose(composePath, compose)

	infos := make([]ServiceInfo, 0, len(compose.Services))
	for serviceName, service := range compose.Services {
		containerName := service.ContainerName
		if containerName == "" {
			// Docker Compose v2 derives: {project}-{service}-{instance}
			containerName = projectName + "-" + serviceName + "-1"
		}

		// Discover port from compose configuration
		port := discoverServicePort(service, envVars)

		infos = append(infos, ServiceInfo{
			ServiceName:   serviceName,
			ContainerName: containerName,
			Profiles:      service.Profiles,
			Port:          port,
		})
	}

	return infos, nil
}

// loadEnvVarsForCompose loads environment variables from env files referenced in the compose file.
func loadEnvVarsForCompose(composePath string, compose *ComposeFile) map[string]string {
	envVars := make(map[string]string)
	projectDir := filepath.Dir(composePath)

	// First, load system environment variables (lowest priority)
	for _, env := range os.Environ() {
		if idx := strings.Index(env, "="); idx > 0 {
			envVars[env[:idx]] = env[idx+1:]
		}
	}

	// Load from common .env file in project directory (if exists)
	defaultEnvPath := filepath.Join(projectDir, ".env")
	loadEnvFile(defaultEnvPath, envVars)

	// Load from env_file directives in each service
	for _, service := range compose.Services {
		for _, envFile := range service.EnvFile {
			envPath := envFile
			if !filepath.IsAbs(envPath) {
				envPath = filepath.Join(projectDir, envFile)
			}
			loadEnvFile(envPath, envVars)
		}
		// Service-level environment vars have highest priority
		maps.Copy(envVars, service.Environment)
	}

	return envVars
}

// loadEnvFile loads environment variables from a .env file into the provided map.
// Silently skips files that do not exist or cannot be opened. A mid-file read
// error is also silently skipped (best-effort env loading for port detection).
func loadEnvFile(path string, envVars map[string]string) {
	file, err := os.Open(path)
	if err != nil {
		return // File doesn't exist or can't be read, skip silently
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Parse KEY=value or KEY="value" or KEY='value'
		if idx := strings.Index(line, "="); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			// Remove surrounding quotes if present
			if len(value) >= 2 {
				if (value[0] == '"' && value[len(value)-1] == '"') ||
					(value[0] == '\'' && value[len(value)-1] == '\'') {
					value = value[1 : len(value)-1]
				}
			}
			envVars[key] = value
		}
	}
	// scanner.Err() is intentionally not propagated: loadEnvFile is best-effort
	// (used for port auto-detection). A partial read still yields useful data.
	_ = scanner.Err()
}

// discoverServicePort attempts to find the container port from compose configuration.
// Returns 0 if no port can be determined.
func discoverServicePort(service ComposeService, envVars map[string]string) int {
	// Priority 1: Check ports mapping (prefer container port)
	if port := extractPortFromPorts(service.Ports, envVars); port > 0 {
		return port
	}

	// Priority 2: Check expose directive
	if port := extractPortFromExpose(service.Expose, envVars); port > 0 {
		return port
	}

	return 0
}

// envVarPattern matches ${VAR}, ${VAR:-default}, ${VAR-default}, $VAR patterns
var envVarPattern = regexp.MustCompile(`\$\{([^}:]+)(?::-?([^}]*))?\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// expandEnvVars replaces environment variable references in a string.
func expandEnvVars(s string, envVars map[string]string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		// Handle ${VAR} or ${VAR:-default} or ${VAR-default}
		if strings.HasPrefix(match, "${") {
			inner := match[2 : len(match)-1]
			var varName, defaultVal string
			if idx := strings.Index(inner, ":-"); idx > 0 {
				varName = inner[:idx]
				defaultVal = inner[idx+2:]
			} else if idx := strings.Index(inner, "-"); idx > 0 {
				varName = inner[:idx]
				defaultVal = inner[idx+1:]
			} else {
				varName = inner
			}
			if val, ok := envVars[varName]; ok && val != "" {
				return val
			}
			return defaultVal
		}
		// Handle $VAR
		if strings.HasPrefix(match, "$") {
			varName := match[1:]
			if val, ok := envVars[varName]; ok {
				return val
			}
		}
		return match
	})
}

// extractPortFromPorts extracts container port from ports configuration.
// Docker Compose port formats:
//   - "80" (container port only)
//   - "8080:80" (host:container)
//   - "8080:80/tcp" (with protocol)
//   - "127.0.0.1:8080:80" (bind:host:container)
//   - "${PORT}:80" (with env var)
func extractPortFromPorts(ports []string, envVars map[string]string) int {
	for _, portSpec := range ports {
		// Expand any environment variables
		portSpec = expandEnvVars(portSpec, envVars)

		// Remove protocol suffix if present
		if idx := strings.Index(portSpec, "/"); idx > 0 {
			portSpec = portSpec[:idx]
		}

		parts := strings.Split(portSpec, ":")
		var containerPort string

		switch len(parts) {
		case 1:
			// Just container port: "80"
			containerPort = parts[0]
		case 2:
			// host:container: "8080:80"
			containerPort = parts[1]
		case 3:
			// bind:host:container: "127.0.0.1:8080:80"
			containerPort = parts[2]
		default:
			continue
		}

		if port, err := strconv.Atoi(strings.TrimSpace(containerPort)); err == nil && port > 0 {
			return port
		}
	}
	return 0
}

// extractPortFromExpose extracts port from expose directive.
func extractPortFromExpose(expose []string, envVars map[string]string) int {
	for _, portSpec := range expose {
		// Expand any environment variables
		portSpec = expandEnvVars(portSpec, envVars)

		// Remove protocol suffix if present
		if idx := strings.Index(portSpec, "/"); idx > 0 {
			portSpec = portSpec[:idx]
		}

		if port, err := strconv.Atoi(strings.TrimSpace(portSpec)); err == nil && port > 0 {
			return port
		}
	}
	return 0
}

// ResolvePath resolves a site path to an absolute path.
func ResolvePath(path string) (string, error) {
	// Expand home directory
	if strings.HasPrefix(path, constants.HomeDirPrefix) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[len(constants.HomeDirPrefix):])
	}

	// Make absolute
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	// Resolve symlinks
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		// Path might not exist yet, return absolute path
		return absPath, nil
	}

	return realPath, nil
}

// IsLocalDomain checks if a domain should use local SSL.
func IsLocalDomain(domain string) bool {
	for _, tld := range traefik.LocalDomains {
		if strings.HasSuffix(domain, "."+tld) {
			return true
		}
	}
	return false
}

// SanitizeName creates a valid site name from a path or string.
// Dots are replaced with hyphens so that a path like "myapp.test" becomes
// "myapp-test", which is a valid site name.
func SanitizeName(s string) string {
	// Use base name if path
	s = filepath.Base(s)
	// Replace invalid characters
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, ".", "-")
	s = strings.ToLower(s)
	return s
}

// Exists checks if a site is already registered.
func Exists(name string) bool {
	return HasSiteMetadata(name)
}

// generateStaticContainerName generates a container name for a static site.
// Format: srv_static_<short_hash> where hash is derived from the site name.
func generateStaticContainerName(name string) string {
	hash := sha256.Sum256([]byte(name))
	shortHash := hex.EncodeToString(hash[:])[:constants.StaticContainerHashLength]
	return constants.StaticContainerPrefix + shortHash
}

// StaticSiteOptions holds configuration options for static sites.
type StaticSiteOptions struct {
	SPA   bool // Enable SPA mode (fallback to index.html)
	Cache bool // Enable caching headers
	CORS  bool // Enable CORS headers
}

// generateStaticNginxConf generates nginx configuration based on options.
func generateStaticNginxConf(opts StaticSiteOptions) string {
	var config strings.Builder

	config.WriteString(`# Generated by srv - static site nginx config
# This file is yours to edit. "srv site regenerate" will reset it.
#
# Common customisations (uncomment to enable):
#
#   client_max_body_size 100M;     # Increase max upload / request body size

server {
    listen 80;
    server_name _;
    root /usr/share/nginx/html;
    index index.html index.htm;

    # Gzip compression
    gzip on;
    gzip_vary on;
    gzip_min_length 1024;
    gzip_types text/plain text/css text/xml text/javascript application/javascript application/json application/xml application/rss+xml application/atom+xml image/svg+xml;

    # Security headers
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-XSS-Protection "1; mode=block" always;
`)

	if opts.CORS {
		config.WriteString(`
    # CORS headers
    add_header Access-Control-Allow-Origin "*" always;
    add_header Access-Control-Allow-Methods "GET, POST, OPTIONS, HEAD" always;
    add_header Access-Control-Allow-Headers "Origin, X-Requested-With, Content-Type, Accept, Authorization" always;

    # Handle preflight requests
    if ($request_method = 'OPTIONS') {
        add_header Access-Control-Allow-Origin "*";
        add_header Access-Control-Allow-Methods "GET, POST, OPTIONS, HEAD";
        add_header Access-Control-Allow-Headers "Origin, X-Requested-With, Content-Type, Accept, Authorization";
        add_header Content-Length 0;
        add_header Content-Type text/plain;
        return 204;
    }
`)
	}

	config.WriteString(`
    # Block access to hidden files (dotfiles)
    location ~ /\. {
        deny all;
        return 404;
    }

    # Block access to sensitive file extensions
    location ~* \.(env|git|gitignore|gitmodules|htaccess|htpasswd|ds_store|yml|yaml|toml|ini|log|sh|sql|bak|swp|tmp)$ {
        deny all;
        return 404;
    }

    # Block access to common sensitive directories
    location ~* ^/(\.git|node_modules|vendor|\.svn|\.hg)/ {
        deny all;
        return 404;
    }

    # Serve static files
    location / {
`)

	if opts.SPA {
		config.WriteString(`        try_files $uri $uri/ /index.html =404;
`)
	} else {
		config.WriteString(`        try_files $uri $uri/ =404;
`)
	}

	config.WriteString(`    }

    # Custom 404 page
    error_page 404 /404.html;
    location = /404.html {
        internal;
    }
`)

	if opts.Cache {
		config.WriteString(`
    # Cache static assets
    location ~* \.(css|js|png|jpg|jpeg|gif|ico|svg|woff|woff2|ttf|eot)$ {
        expires 1y;
        add_header Cache-Control "public, immutable";
    }
`)
	} else {
		config.WriteString(`
    # No caching (development mode)
    add_header Cache-Control "no-cache, no-store, must-revalidate" always;
    add_header Pragma "no-cache" always;
    add_header Expires "0" always;
`)
	}

	config.WriteString(`}
`)

	return config.String()
}

// =============================================================================
// Site Metadata (stored in ~/.config/srv/sites/{name}/)
// =============================================================================

// SiteType represents the type of site being served.
type SiteType string

const (
	SiteTypeCompose    SiteType = constants.SiteTypeCompose    // Docker compose project
	SiteTypeStatic     SiteType = constants.SiteTypeStatic     // Static files served via nginx
	SiteTypePHP        SiteType = constants.SiteTypePHP        // PHP/FPM site (nginx + php-fpm)
	SiteTypeNode       SiteType = constants.SiteTypeNode       // Node.js / Bun / Deno site
	SiteTypeRuby       SiteType = constants.SiteTypeRuby       // Ruby site
	SiteTypePython     SiteType = constants.SiteTypePython     // Python site
	SiteTypeDockerfile SiteType = constants.SiteTypeDockerfile // Dockerfile site
)

// Limits holds optional per-site/per-route timeout and request-body limits.
// All fields use string forms ("2G", "300s") so YAML stays human-readable and
// the values pass through to nginx/Traefik in their native syntax.
type Limits struct {
	MaxBody        string `yaml:"max_body,omitempty"`        // e.g. "2G", "128M"
	ReadTimeout    string `yaml:"read_timeout,omitempty"`    // e.g. "300s"
	SendTimeout    string `yaml:"send_timeout,omitempty"`
	ConnectTimeout string `yaml:"connect_timeout,omitempty"`
}

// Upstream points a route or proxy at a backend. Exactly one of Port/Container/URL
// is set per Kind.
type Upstream struct {
	Kind      string `yaml:"kind"`                // "localhost" | "container" | "url"
	Port      int    `yaml:"port,omitempty"`      // when kind=localhost or kind=container
	Container string `yaml:"container,omitempty"` // when kind=container
	URL       string `yaml:"url,omitempty"`       // when kind=url
}

// Route attaches an extra Traefik router to a site, used for path-prefix splits
// (e.g. /app → WebSocket on :6001) or regex rewrites (e.g. /videos/...).
type Route struct {
	ID               string   `yaml:"id"`                            // stable handle for CLI
	Path             string   `yaml:"path,omitempty"`                // PathPrefix
	PathRegex        string   `yaml:"path_regex,omitempty"`          // PathRegexp
	Rewrite          string   `yaml:"rewrite,omitempty"`             // ReplacePathRegex replacement
	Upstream         Upstream `yaml:"upstream"`
	PreserveHost     *bool    `yaml:"preserve_host,omitempty"`       // tri-state; nil = default true
	PassRangeHeaders bool     `yaml:"pass_range_headers,omitempty"`
	Priority         int      `yaml:"priority,omitempty"`            // optional Traefik priority override
	Limits           *Limits  `yaml:"limits,omitempty"`              // per-route override
}

// Fallback configures a remote upstream that takes over when the primary
// upstream returns a 5xx response. Implemented via a small nginx sidecar in
// front of the main upstream.
type Fallback struct {
	URL     string `yaml:"url"`
	Timeout string `yaml:"timeout,omitempty"` // e.g. "2s"
}

// CurrentMetadataSchema is the version written to new metadata.yml files. Bump
// when introducing a breaking, non-additive change.
const CurrentMetadataSchema = 1

// SiteMetadata holds all configuration for a site.
// This is stored in ~/.config/srv/sites/{name}/metadata.yml
type SiteMetadata struct {
	SchemaVersion      int      `yaml:"schema_version,omitempty"`       // metadata.yml schema (1 = current)
	Type               SiteType `yaml:"type"`                           // compose or static
	Domains            []string `yaml:"domains,omitempty"`              // All hostnames; Domains[0] is canonical
	ProjectPath        string   `yaml:"project_path"`                   // Absolute path to the project
	ServiceName        string   `yaml:"service_name,omitempty"`         // Container name (for Traefik routing)
	ComposeServiceName string   `yaml:"compose_service_name,omitempty"` // Docker Compose service name (for compose commands)
	Profile            string   `yaml:"profile,omitempty"`              // Docker Compose profile (if service uses profiles)
	Port               int      `yaml:"port"`                           // Port the service listens on
	IsLocal            bool     `yaml:"is_local"`                       // Whether to use local SSL
	Wildcard           bool     `yaml:"wildcard,omitempty"`             // Match apex + one-level subdomains
	NetworkName        string   `yaml:"network_name"`                   // Docker network name
	// Listeners enables extra entrypoints for this site (e.g. ["internal"] for :88 plain HTTP).
	Listeners []string `yaml:"listeners,omitempty"`
	// Limits applies request-body and timeout overrides.
	Limits *Limits `yaml:"limits,omitempty"`
	// Routes are extra Traefik routers attached to this host (path-prefix / regex-rewrite splits).
	Routes []Route `yaml:"routes,omitempty"`
	// Upstream is set on proxy-type sites to describe the primary backend.
	Upstream *Upstream `yaml:"upstream,omitempty"`
	// Fallback (optional) describes a remote 5xx-fallback target for proxy sites.
	Fallback *Fallback `yaml:"fallback,omitempty"`
	// Static site options
	SPA   bool `yaml:"spa,omitempty"`   // Enable SPA mode
	Cache bool `yaml:"cache,omitempty"` // Enable caching headers
	CORS  bool `yaml:"cors,omitempty"`  // Enable CORS headers
	// PHP site options
	PHPVersion    string   `yaml:"php_version,omitempty"`    // PHP version ("latest" or "8.3")
	PHPExtensions []string `yaml:"php_extensions,omitempty"` // PHP extensions to install
	PHPFramework  string   `yaml:"php_framework,omitempty"`  // Detected framework
	DocumentRoot  string   `yaml:"document_root,omitempty"`  // Document root relative to project
	// Node.js / Bun / Deno site options
	NodeRuntime        string `yaml:"node_runtime,omitempty"`         // Runtime: "node", "bun", "deno"
	NodePackageManager string `yaml:"node_package_manager,omitempty"` // PM: "npm", "yarn", "pnpm", "bun", "deno"
	NodeVersion        string `yaml:"node_version,omitempty"`         // Node version ("lts" or "20"; node runtime only)
	NodeFramework      string `yaml:"node_framework,omitempty"`       // Detected framework
	NodeStartCmd       string `yaml:"node_start_cmd,omitempty"`       // Start command (e.g. "npm run dev")
	// Ruby site options
	RubyVersion   string `yaml:"ruby_version,omitempty"`   // Ruby version ("latest" or "3.3")
	RubyFramework string `yaml:"ruby_framework,omitempty"` // Detected framework (rails/sinatra/rack/generic)
	RubyStartCmd  string `yaml:"ruby_start_cmd,omitempty"` // Start command
	// Python site options
	PythonVersion   string `yaml:"python_version,omitempty"`   // Python version ("latest" or "3.12")
	PythonFramework string `yaml:"python_framework,omitempty"` // Detected framework (django/fastapi/flask/generic)
	PythonStartCmd  string `yaml:"python_start_cmd,omitempty"` // Start command
	// Dockerfile site options
	DockerfilePort int `yaml:"dockerfile_port,omitempty"` // Port from EXPOSE directive
}

// PrimaryDomain returns the canonical (first) domain registered for the site,
// or "" if none is configured.
func (m *SiteMetadata) PrimaryDomain() string {
	if m == nil || len(m.Domains) == 0 {
		return ""
	}
	return m.Domains[0]
}

// SiteConfigDir returns the path to a site's configuration directory.
func SiteConfigDir(cfg *config.Config, name string) string {
	return filepath.Join(cfg.SitesDir, name)
}

// metadataPath returns the path to a site's metadata file.
func metadataPath(cfg *config.Config, name string) string {
	return filepath.Join(SiteConfigDir(cfg, name), constants.MetadataFile)
}

// SiteComposePath returns the path to a site's docker-compose.yml (for static sites).
func SiteComposePath(cfg *config.Config, name string) string {
	return filepath.Join(SiteConfigDir(cfg, name), constants.DockerComposeFile)
}

// SiteNginxConfPath returns the path to a site's nginx.conf (for static sites).
func SiteNginxConfPath(cfg *config.Config, name string) string {
	return filepath.Join(SiteConfigDir(cfg, name), constants.NginxConfFile)
}

// WriteSiteMetadata writes metadata for a site. SchemaVersion is stamped to the
// current schema if not already set.
func WriteSiteMetadata(name string, meta SiteMetadata) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if meta.SchemaVersion == 0 {
		meta.SchemaVersion = CurrentMetadataSchema
	}

	// Ensure site config directory exists
	siteDir := SiteConfigDir(cfg, name)
	if err := os.MkdirAll(siteDir, constants.DirPermDefault); err != nil {
		return fmt.Errorf("failed to create site config directory: %w", err)
	}

	data, err := yaml.Marshal(&meta)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	header := "# Site metadata - generated by srv\n"
	content := header + string(data)

	return os.WriteFile(metadataPath(cfg, name), []byte(content), constants.FilePermDefault)
}

// ReadSiteMetadata reads metadata for a site.
// Returns nil if the metadata file doesn't exist.
//
// Older metadata.yml files used a scalar `domain:` field. They are migrated
// transparently in-memory; the on-disk file is only rewritten on the next
// mutation. Unknown keys are ignored (lenient parsing).
func ReadSiteMetadata(name string) (*SiteMetadata, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(metadataPath(cfg, name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var meta SiteMetadata
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	// Legacy migration: pre-schema-1 metadata had `domain: foo` instead of
	// `domains: [foo]`. Detect via a second pass and populate Domains.
	if len(meta.Domains) == 0 {
		var legacy struct {
			Domain string `yaml:"domain"`
		}
		if err := yaml.Unmarshal(data, &legacy); err == nil && legacy.Domain != "" {
			meta.Domains = []string{legacy.Domain}
		}
	}

	return &meta, nil
}

// RemoveSiteMetadata removes all configuration for a site.
func RemoveSiteMetadata(name string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	siteDir := SiteConfigDir(cfg, name)
	if err := os.RemoveAll(siteDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove site config: %w", err)
	}
	return nil
}

// HasSiteMetadata checks if a site has metadata stored.
func HasSiteMetadata(name string) bool {
	meta, err := ReadSiteMetadata(name)
	return err == nil && meta != nil
}

// =============================================================================
// Static Site Configuration Types
// =============================================================================

// volumeConsistencyForHost returns "cached" on macOS so bind mounts trade
// strict host→container consistency for far better I/O throughput. Empty
// elsewhere (Docker ignores the field outside of Docker Desktop).
func volumeConsistencyForHost() string {
	if runtime.GOOS == "darwin" {
		return "cached"
	}
	return ""
}

// staticVolumeConfig represents a volume in docker-compose.
type staticVolumeConfig struct {
	Type        string `yaml:"type"`
	Source      string `yaml:"source"`
	Target      string `yaml:"target"`
	ReadOnly    bool   `yaml:"read_only"`
	Consistency string `yaml:"consistency,omitempty"`
}

// staticServiceConfig represents a service in docker-compose.
type staticServiceConfig struct {
	ContainerName string               `yaml:"container_name"`
	Image         string               `yaml:"image"`
	Volumes       []staticVolumeConfig `yaml:"volumes"`
	Labels        map[string]string    `yaml:"labels"`
	Networks      []string             `yaml:"networks"`
	Restart       string               `yaml:"restart"`
	HealthCheck   *staticHealthCheck   `yaml:"healthcheck,omitempty"`
}

// staticHealthCheck mirrors the compose healthcheck shape. Kept separate from
// phpHealthCheck so static sites don't pull in the FPM-specific helper.
type staticHealthCheck struct {
	Test        []string `yaml:"test"`
	Interval    string   `yaml:"interval,omitempty"`
	Timeout     string   `yaml:"timeout,omitempty"`
	StartPeriod string   `yaml:"start_period,omitempty"`
	Retries     int      `yaml:"retries,omitempty"`
}

// makeStaticHealthCheck builds a TCP-probe healthcheck for the given port.
// busybox `nc` ships in every alpine image srv currently uses.
func makeStaticHealthCheck(port int) *staticHealthCheck {
	return &staticHealthCheck{
		Test:        []string{"CMD-SHELL", fmt.Sprintf("nc -z 127.0.0.1 %d || exit 1", port)},
		Interval:    "30s",
		Timeout:     "3s",
		StartPeriod: "5s",
		Retries:     3,
	}
}

// staticNetworkConfig represents a network in docker-compose.
type staticNetworkConfig struct {
	Name     string `yaml:"name"`
	External bool   `yaml:"external"`
}

// staticComposeConfig represents a docker-compose.yml for static sites.
type staticComposeConfig struct {
	Services map[string]staticServiceConfig `yaml:"services"`
	Networks map[string]staticNetworkConfig `yaml:"networks"`
}

// buildStaticTraefikLabels builds Traefik labels for a static site.
func buildStaticTraefikLabels(name string, domains []string, isLocal, wildcard bool) map[string]string {
	labels := map[string]string{
		"traefik.enable": "true",
		fmt.Sprintf("traefik.http.routers.%s.rule", name):                      traefik.BuildHostRule(domains, wildcard),
		fmt.Sprintf("traefik.http.routers.%s.entrypoints", name):               "websecure",
		fmt.Sprintf("traefik.http.routers.%s.tls", name):                       "true",
		fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", name): "80",
	}
	if !isLocal {
		labels[fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", name)] = "letsencrypt"
	}
	return labels
}

// HasListener reports whether the supplied listener name is enabled on the
// site. Comparison is case-insensitive.
func HasListener(listeners []string, name string) bool {
	for _, l := range listeners {
		if strings.EqualFold(l, name) {
			return true
		}
	}
	return false
}

// addInternalListenerLabels appends labels for a plain-HTTP router on the
// `internal` entrypoint, sharing the site's existing Traefik service. Called
// when the site opts in via `listeners: [internal]`.
func addInternalListenerLabels(labels map[string]string, name string, domains []string, wildcard bool) {
	router := name + "-internal"
	labels[fmt.Sprintf("traefik.http.routers.%s.rule", router)] = traefik.BuildHostRule(domains, wildcard)
	labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", router)] = constants.EntryPointInternal
	labels[fmt.Sprintf("traefik.http.routers.%s.service", router)] = name
}

// buildStaticComposeConfig builds the docker-compose configuration for a static site.
func buildStaticComposeConfig(containerName, projectPath, nginxConfPath, networkName string, labels map[string]string) staticComposeConfig {
	return staticComposeConfig{
		Services: map[string]staticServiceConfig{
			"web": {
				ContainerName: containerName,
				Image:         constants.ImageNginxAlpine,
				Volumes: []staticVolumeConfig{
					{
						Type:        "bind",
						Source:      projectPath,
						Target:      constants.NginxHTMLPath,
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
				HealthCheck: makeStaticHealthCheck(80),
			},
		},
		Networks: map[string]staticNetworkConfig{
			constants.TraefikSubdir: {
				Name:     networkName,
				External: true,
			},
		},
	}
}

// writeFile writes content to path.
// If force is false and the file already exists, the write is skipped.
func writeFile(path string, content []byte, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return nil // file exists — user may have customized it
		}
	}
	return os.WriteFile(path, content, constants.FilePermDefault)
}

// WriteStaticSiteConfig writes the docker-compose.yml and nginx.conf for a static site.
// If force is false, existing files are left untouched so user edits are preserved.
func WriteStaticSiteConfig(name string, meta SiteMetadata, force bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	siteDir := SiteConfigDir(cfg, name)
	if err := os.MkdirAll(siteDir, constants.DirPermDefault); err != nil {
		return fmt.Errorf("failed to create site config directory: %w", err)
	}

	// Generate and write nginx config
	nginxConf := generateStaticNginxConf(StaticSiteOptions{
		SPA:   meta.SPA,
		Cache: meta.Cache,
		CORS:  meta.CORS,
	})
	nginxConfPath := SiteNginxConfPath(cfg, name)
	if err := writeFile(nginxConfPath, []byte(nginxConf), force); err != nil {
		return fmt.Errorf("failed to write nginx.conf: %w", err)
	}

	// Build and write docker-compose.yml
	containerName := generateStaticContainerName(name)
	labels := buildStaticTraefikLabels(name, meta.Domains, meta.IsLocal, meta.Wildcard)
	if HasListener(meta.Listeners, constants.ListenerInternal) {
		addInternalListenerLabels(labels, name, meta.Domains, meta.Wildcard)
	}
	composeConfig := buildStaticComposeConfig(containerName, meta.ProjectPath, nginxConfPath, meta.NetworkName, labels)

	data, err := yaml.Marshal(&composeConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal compose config: %w", err)
	}

	header := fmt.Sprintf("# Generated by srv - static site\n# Project: %s\n#\n# This file is yours to edit. Changes take effect on next restart.\n\n", meta.ProjectPath)
	content := header + string(data)

	composePath := SiteComposePath(cfg, name)
	return writeFile(composePath, []byte(content), force)
}
