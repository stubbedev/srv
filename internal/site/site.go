// Package site handles site management operations.
package site

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	Domain             string   // Domain from metadata
	IsLocal            bool     // Whether it uses local SSL
	Type               SiteType // compose or static
	IsBroken           bool     // Whether the project directory exists
	Status             string   // Container status
	ServiceName        string   // Container name (for Traefik routing)
	ComposeServiceName string   // Docker Compose service name (for compose commands)
	Profile            string   // Docker Compose profile (if service uses profiles)
	Port               int      // Port (for compose sites)
	ComposeDir         string   // Directory containing docker-compose.yml (may differ from Dir for static sites)
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

	s.Domain = meta.Domain
	s.IsLocal = meta.IsLocal
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
	if meta.Type == SiteTypeStatic {
		// Static sites have compose file in srv config dir
		s.ComposeDir = SiteConfigDir(cfg, entry.Name())
	} else {
		// Compose sites use the project directory
		s.ComposeDir = meta.ProjectPath
	}

	return s, true // Needs status check
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
			sem <- struct{}{}        // Acquire
			defer func() { <-sem }() // Release

			// Use ComposeDir for status check
			status := docker.ContainerStatus(sites[i].ComposeDir)
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
		// Only process directories (site config dirs)
		if !entry.IsDir() {
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

// FindComposeFile finds the docker-compose file in a directory.
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
		}
	}

	return "", fmt.Errorf("no docker-compose file found in %s", dir)
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

	// Get the project name (directory name) for deriving container names
	projectDir := filepath.Dir(composePath)
	projectName := strings.ToLower(filepath.Base(projectDir))
	// Docker Compose uses underscores in derived names
	projectName = strings.ReplaceAll(projectName, "-", "_")

	// Load environment variables from env files and environment
	envVars := loadEnvVarsForCompose(composePath, compose)

	infos := make([]ServiceInfo, 0, len(compose.Services))
	for serviceName, service := range compose.Services {
		containerName := service.ContainerName
		if containerName == "" {
			// Docker Compose derives container name as: {project}_{service}_{instance}
			// For single instances, it's typically: {project}-{service}-1
			// But the DNS name is just: {project}-{service} or {project}_{service}
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
		for k, v := range service.Environment {
			envVars[k] = v
		}
	}

	return envVars
}

// loadEnvFile loads environment variables from a .env file into the provided map.
func loadEnvFile(path string, envVars map[string]string) {
	file, err := os.Open(path)
	if err != nil {
		return // File doesn't exist or can't be read, skip silently
	}
	defer file.Close()

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
func SanitizeName(s string) string {
	// Use base name if path
	s = filepath.Base(s)
	// Replace invalid characters
	s = strings.ReplaceAll(s, " ", "-")
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
	SiteTypeCompose SiteType = constants.SiteTypeCompose // Docker compose project
	SiteTypeStatic  SiteType = constants.SiteTypeStatic  // Static files served via nginx
)

// SiteMetadata holds all configuration for a site.
// This is stored in ~/.config/srv/sites/{name}/metadata.yml
type SiteMetadata struct {
	Type               SiteType `yaml:"type"`                           // compose or static
	Domain             string   `yaml:"domain"`                         // Domain to serve on
	ProjectPath        string   `yaml:"project_path"`                   // Absolute path to the project
	ServiceName        string   `yaml:"service_name,omitempty"`         // Container name (for Traefik routing)
	ComposeServiceName string   `yaml:"compose_service_name,omitempty"` // Docker Compose service name (for compose commands)
	Profile            string   `yaml:"profile,omitempty"`              // Docker Compose profile (if service uses profiles)
	Port               int      `yaml:"port"`                           // Port the service listens on
	IsLocal            bool     `yaml:"is_local"`                       // Whether to use local SSL
	NetworkName        string   `yaml:"network_name"`                   // Docker network name
	// Static site options
	SPA   bool `yaml:"spa,omitempty"`   // Enable SPA mode
	Cache bool `yaml:"cache,omitempty"` // Enable caching headers
	CORS  bool `yaml:"cors,omitempty"`  // Enable CORS headers
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

// WriteSiteMetadata writes metadata for a site.
func WriteSiteMetadata(name string, meta SiteMetadata) error {
	cfg, err := config.Load()
	if err != nil {
		return err
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

// staticVolumeConfig represents a volume in docker-compose.
type staticVolumeConfig struct {
	Type     string `yaml:"type"`
	Source   string `yaml:"source"`
	Target   string `yaml:"target"`
	ReadOnly bool   `yaml:"read_only"`
}

// staticServiceConfig represents a service in docker-compose.
type staticServiceConfig struct {
	ContainerName string               `yaml:"container_name"`
	Image         string               `yaml:"image"`
	Volumes       []staticVolumeConfig `yaml:"volumes"`
	Labels        map[string]string    `yaml:"labels"`
	Networks      []string             `yaml:"networks"`
	Restart       string               `yaml:"restart"`
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
func buildStaticTraefikLabels(name, domain string, isLocal bool) map[string]string {
	labels := map[string]string{
		"traefik.enable": "true",
		fmt.Sprintf("traefik.http.routers.%s.rule", name):                      fmt.Sprintf("Host(`%s`)", domain),
		fmt.Sprintf("traefik.http.routers.%s.entrypoints", name):               "websecure",
		fmt.Sprintf("traefik.http.routers.%s.tls", name):                       "true",
		fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", name): "80",
	}
	if !isLocal {
		labels[fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", name)] = "letsencrypt"
	}
	return labels
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
						Type:     "bind",
						Source:   projectPath,
						Target:   constants.NginxHTMLPath,
						ReadOnly: true,
					},
					{
						Type:     "bind",
						Source:   nginxConfPath,
						Target:   constants.NginxDefaultConfPath,
						ReadOnly: true,
					},
				},
				Labels:   labels,
				Networks: []string{constants.TraefikSubdir},
				Restart:  constants.RestartUnlessStopped,
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

// WriteStaticSiteConfig writes the docker-compose.yml and nginx.conf for a static site.
func WriteStaticSiteConfig(name string, meta SiteMetadata) error {
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
	if err := os.WriteFile(nginxConfPath, []byte(nginxConf), constants.FilePermDefault); err != nil {
		return fmt.Errorf("failed to write nginx.conf: %w", err)
	}

	// Build and write docker-compose.yml
	containerName := generateStaticContainerName(name)
	labels := buildStaticTraefikLabels(name, meta.Domain, meta.IsLocal)
	composeConfig := buildStaticComposeConfig(containerName, meta.ProjectPath, nginxConfPath, meta.NetworkName, labels)

	data, err := yaml.Marshal(&composeConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal compose config: %w", err)
	}

	header := fmt.Sprintf(`# Generated by srv - static site
# Project: %s
# Do not edit - changes will be overwritten

`, meta.ProjectPath)
	content := header + string(data)

	composePath := SiteComposePath(cfg, name)
	return os.WriteFile(composePath, []byte(content), constants.FilePermDefault)
}
