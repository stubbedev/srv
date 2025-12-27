// Package site handles site management operations.
package site

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/traefik"
)

// Site represents a registered site.
type Site struct {
	Name     string // Name of the site (symlink name)
	Dir      string // Resolved directory path
	Domain   string // Domain from env.site
	IsLocal  bool   // Whether it uses local SSL
	IsBroken bool   // Whether the symlink target exists
	Status   string // Container status
	LinkPath string // Path to the symlink
}

// SiteConfig holds configuration for adding a new site.
type SiteConfig struct {
	Path           string
	Domain         string
	Port           string
	Name           string
	ServiceName    string
	IsLocal        bool
	Start          bool
	Force          bool
	SkipValidation bool
}

// ComposeFile represents a docker-compose.yml structure.
type ComposeFile struct {
	Services map[string]ComposeService `yaml:"services"`
}

// ComposeService represents a service in docker-compose.
type ComposeService struct {
	Image  string        `yaml:"image"`
	Labels ComposeLabels `yaml:"labels"`
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
		if entry.Type()&os.ModeSymlink == 0 {
			continue
		}

		linkPath := filepath.Join(cfg.SitesDir, entry.Name())
		s := Site{
			Name:     entry.Name(),
			LinkPath: linkPath,
		}

		// Resolve symlink
		target, err := os.Readlink(linkPath)
		if err != nil {
			s.IsBroken = true
			sites = append(sites, s)
			continue
		}

		// Make absolute if relative
		if !filepath.IsAbs(target) {
			target = filepath.Join(cfg.SitesDir, target)
		}

		// Check if target exists
		if _, err := os.Stat(target); err != nil {
			s.IsBroken = true
			sites = append(sites, s)
			continue
		}

		s.Dir = target

		// Parse env.site for domain info
		domain, isLocal := ParseEnv(target)
		s.Domain = domain
		s.IsLocal = isLocal

		// Mark for status check
		validSiteIndices = append(validSiteIndices, len(sites))
		sites = append(sites, s)
	}

	// Second pass: fetch container status in parallel
	if len(validSiteIndices) > 0 {
		const maxWorkers = 8
		workers := min(maxWorkers, len(validSiteIndices))

		var wg sync.WaitGroup
		statusChan := make(chan struct {
			index  int
			status string
		}, len(validSiteIndices))

		// Semaphore for limiting concurrency
		sem := make(chan struct{}, workers)

		for _, idx := range validSiteIndices {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				sem <- struct{}{}        // Acquire
				defer func() { <-sem }() // Release

				status := docker.ContainerStatus(sites[i].Dir)
				statusChan <- struct {
					index  int
					status string
				}{i, status}
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

// ParseEnv reads env.site and returns domain and isLocal.
// Handles both quoted and unquoted values for backward compatibility.
func ParseEnv(dir string) (domain string, isLocal bool) {
	envPath := filepath.Join(dir, "env.site")
	file, err := os.Open(envPath)
	if err != nil {
		return "", false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Remove surrounding quotes if present
		value = unquoteEnvValue(value)

		switch key {
		case "DEPLOY_DOMAIN":
			domain = value
		case "DEPLOY_LOCAL":
			isLocal = value == "true" || value == "1"
		}
	}

	return domain, isLocal
}

// unquoteEnvValue removes surrounding quotes from an env value.
func unquoteEnvValue(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
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

// GetServiceNames returns service names from a compose file.
func GetServiceNames(composePath string) ([]string, error) {
	compose, err := ParseComposeFile(composePath)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(compose.Services))
	for name := range compose.Services {
		names = append(names, name)
	}

	return names, nil
}

// ResolvePath resolves a site path to an absolute path.
func ResolvePath(path string) (string, error) {
	// Expand home directory
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[2:])
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
	cfg, err := config.Load()
	if err != nil {
		return false
	}

	linkPath := filepath.Join(cfg.SitesDir, name)
	_, err = os.Lstat(linkPath)
	return err == nil
}

// Register creates a symlink for a site.
func Register(name, targetDir string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Ensure sites directory exists
	if err := os.MkdirAll(cfg.SitesDir, 0o755); err != nil {
		return fmt.Errorf("failed to create sites directory: %w", err)
	}

	linkPath := filepath.Join(cfg.SitesDir, name)
	if err := os.Symlink(targetDir, linkPath); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}

	return nil
}

// Unregister removes a site's symlink.
func Unregister(name string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	linkPath := filepath.Join(cfg.SitesDir, name)
	if err := os.Remove(linkPath); err != nil {
		return fmt.Errorf("failed to remove symlink: %w", err)
	}

	return nil
}

// WriteEnvFile writes the env.site file for a site.
// Values are quoted to prevent shell injection issues.
func WriteEnvFile(dir string, domain string, isLocal bool, networkName string) error {
	envPath := filepath.Join(dir, "env.site")

	localStr := "false"
	if isLocal {
		localStr = "true"
	}

	// Quote values to handle special characters safely
	content := fmt.Sprintf(`# Generated by srv - do not edit
DEPLOY_DOMAIN=%q
DEPLOY_LOCAL=%s
DEPLOY_NETWORK=%q
`, domain, localStr, networkName)

	return os.WriteFile(envPath, []byte(content), 0o644)
}

// WriteSiteCompose writes the docker-compose.site.yml file for a site.
// Uses proper YAML marshaling to prevent injection attacks.
func WriteSiteCompose(dir, serviceName, name, domain, port string, isLocal bool, networkName string) error {
	composePath := filepath.Join(dir, "docker-compose.site.yml")

	// Build labels map
	labels := map[string]string{
		"traefik.enable": "true",
		fmt.Sprintf("traefik.http.routers.%s.rule", name):                      fmt.Sprintf("Host(`%s`)", domain),
		fmt.Sprintf("traefik.http.routers.%s.entrypoints", name):               "websecure",
		fmt.Sprintf("traefik.http.routers.%s.tls", name):                       "true",
		fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", name): port,
	}
	if !isLocal {
		labels[fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", name)] = "letsencrypt"
	}

	// Build compose structure
	type ServiceConfig struct {
		Labels   map[string]string `yaml:"labels"`
		Networks []string          `yaml:"networks"`
	}
	type NetworkConfig struct {
		Name     string `yaml:"name"`
		External bool   `yaml:"external"`
	}
	type ComposeConfig struct {
		Services map[string]ServiceConfig `yaml:"services"`
		Networks map[string]NetworkConfig `yaml:"networks"`
	}

	config := ComposeConfig{
		Services: map[string]ServiceConfig{
			serviceName: {
				Labels:   labels,
				Networks: []string{"traefik"},
			},
		},
		Networks: map[string]NetworkConfig{
			"traefik": {
				Name:     networkName,
				External: true,
			},
		},
	}

	data, err := yaml.Marshal(&config)
	if err != nil {
		return fmt.Errorf("failed to marshal compose config: %w", err)
	}

	// Add header comment
	header := `# Generated by srv - do not edit
# Include this in your docker-compose.yml:
#   include:
#     - docker-compose.site.yml

`
	content := header + string(data)

	return os.WriteFile(composePath, []byte(content), 0o644)
}

// RemoveGeneratedFiles removes env.site, docker-compose.site.yml, and static site docker-compose.yml.
func RemoveGeneratedFiles(dir string) {
	os.Remove(filepath.Join(dir, "env.site"))
	os.Remove(filepath.Join(dir, "docker-compose.site.yml"))
	// Also remove static site compose if present
	RemoveStaticSiteCompose(dir)
}

// IsStaticSite checks if a site is a static file site (generated docker-compose.yml).
func IsStaticSite(dir string) bool {
	composePath := filepath.Join(dir, "docker-compose.yml")
	data, err := os.ReadFile(composePath)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "# Generated by srv - static site")
}

// WriteStaticSiteCompose writes a docker-compose.yml file for serving static files with nginx.
func WriteStaticSiteCompose(dir, name, domain, networkName string) error {
	composePath := filepath.Join(dir, "docker-compose.yml")

	// Build labels map
	labels := map[string]string{
		"traefik.enable": "true",
		fmt.Sprintf("traefik.http.routers.%s.rule", name):                      fmt.Sprintf("Host(`%s`)", domain),
		fmt.Sprintf("traefik.http.routers.%s.entrypoints", name):               "websecure",
		fmt.Sprintf("traefik.http.routers.%s.tls", name):                       "true",
		fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", name): "80",
	}

	// Build compose structure
	type VolumeConfig struct {
		Type     string `yaml:"type"`
		Source   string `yaml:"source"`
		Target   string `yaml:"target"`
		ReadOnly bool   `yaml:"read_only"`
	}
	type ServiceConfig struct {
		Image    string            `yaml:"image"`
		Volumes  []VolumeConfig    `yaml:"volumes"`
		Labels   map[string]string `yaml:"labels"`
		Networks []string          `yaml:"networks"`
		Restart  string            `yaml:"restart"`
	}
	type NetworkConfig struct {
		Name     string `yaml:"name"`
		External bool   `yaml:"external"`
	}
	type ComposeConfig struct {
		Services map[string]ServiceConfig `yaml:"services"`
		Networks map[string]NetworkConfig `yaml:"networks"`
	}

	config := ComposeConfig{
		Services: map[string]ServiceConfig{
			"web": {
				Image: "nginx:alpine",
				Volumes: []VolumeConfig{
					{
						Type:     "bind",
						Source:   ".",
						Target:   "/usr/share/nginx/html",
						ReadOnly: true,
					},
				},
				Labels:   labels,
				Networks: []string{"traefik"},
				Restart:  "unless-stopped",
			},
		},
		Networks: map[string]NetworkConfig{
			"traefik": {
				Name:     networkName,
				External: true,
			},
		},
	}

	data, err := yaml.Marshal(&config)
	if err != nil {
		return fmt.Errorf("failed to marshal compose config: %w", err)
	}

	// Add header comment
	header := `# Generated by srv - static site
# This file serves the current directory as static files using nginx.
# To customize, edit this file and restart the site.

`
	content := header + string(data)

	return os.WriteFile(composePath, []byte(content), 0o644)
}

// RemoveStaticSiteCompose removes the generated docker-compose.yml for static sites.
func RemoveStaticSiteCompose(dir string) error {
	if !IsStaticSite(dir) {
		return nil // Not a static site, don't remove
	}
	composePath := filepath.Join(dir, "docker-compose.yml")
	return os.Remove(composePath)
}

// EnsureSiteComposeInclude adds docker-compose.site.yml to the include section
// of the site's docker-compose file if not already present.
// Returns true if the include was added, false if already present.
func EnsureSiteComposeInclude(composePath string) (bool, error) {
	const includeFile = "docker-compose.site.yml"

	data, err := os.ReadFile(composePath)
	if err != nil {
		return false, fmt.Errorf("failed to read compose file: %w", err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return false, fmt.Errorf("failed to parse compose file: %w", err)
	}

	// Root should be a document node containing a mapping
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return false, fmt.Errorf("invalid compose file structure")
	}

	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return false, fmt.Errorf("compose file root is not a mapping")
	}

	// Find or create the include key
	var includeNode *yaml.Node
	for i := 0; i < len(doc.Content); i += 2 {
		if doc.Content[i].Value == "include" {
			includeNode = doc.Content[i+1]
			break
		}
	}

	if includeNode != nil {
		// Check if already included
		if includeNode.Kind == yaml.SequenceNode {
			for _, item := range includeNode.Content {
				if item.Value == includeFile {
					return false, nil // Already present
				}
			}
			// Add to existing sequence
			includeNode.Content = append(includeNode.Content, &yaml.Node{
				Kind:  yaml.ScalarNode,
				Value: includeFile,
				Tag:   "!!str",
			})
		} else {
			return false, fmt.Errorf("include is not a sequence")
		}
	} else {
		// Create new include section at the beginning
		keyNode := &yaml.Node{
			Kind:  yaml.ScalarNode,
			Value: "include",
			Tag:   "!!str",
		}
		valueNode := &yaml.Node{
			Kind: yaml.SequenceNode,
			Tag:  "!!seq",
			Content: []*yaml.Node{
				{
					Kind:  yaml.ScalarNode,
					Value: includeFile,
					Tag:   "!!str",
				},
			},
		}
		// Prepend to document
		doc.Content = append([]*yaml.Node{keyNode, valueNode}, doc.Content...)
	}

	// Marshal back to YAML
	out, err := yaml.Marshal(&root)
	if err != nil {
		return false, fmt.Errorf("failed to marshal compose file: %w", err)
	}

	if err := os.WriteFile(composePath, out, 0o644); err != nil {
		return false, fmt.Errorf("failed to write compose file: %w", err)
	}

	return true, nil
}

// RemoveSiteComposeInclude removes docker-compose.site.yml from the include section
// of the site's docker-compose file.
// Returns true if the include was removed, false if not present.
func RemoveSiteComposeInclude(composePath string) (bool, error) {
	const includeFile = "docker-compose.site.yml"

	data, err := os.ReadFile(composePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to read compose file: %w", err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return false, fmt.Errorf("failed to parse compose file: %w", err)
	}

	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return false, nil
	}

	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return false, nil
	}

	// Find include key
	var includeIdx int = -1
	var includeNode *yaml.Node
	for i := 0; i < len(doc.Content); i += 2 {
		if doc.Content[i].Value == "include" {
			includeIdx = i
			includeNode = doc.Content[i+1]
			break
		}
	}

	if includeNode == nil || includeNode.Kind != yaml.SequenceNode {
		return false, nil
	}

	// Find and remove the include file
	found := false
	newContent := make([]*yaml.Node, 0, len(includeNode.Content))
	for _, item := range includeNode.Content {
		if item.Value == includeFile {
			found = true
		} else {
			newContent = append(newContent, item)
		}
	}

	if !found {
		return false, nil
	}

	if len(newContent) == 0 {
		// Remove entire include section if empty
		doc.Content = append(doc.Content[:includeIdx], doc.Content[includeIdx+2:]...)
	} else {
		includeNode.Content = newContent
	}

	// Marshal back
	out, err := yaml.Marshal(&root)
	if err != nil {
		return false, fmt.Errorf("failed to marshal compose file: %w", err)
	}

	if err := os.WriteFile(composePath, out, 0o644); err != nil {
		return false, fmt.Errorf("failed to write compose file: %w", err)
	}

	return true, nil
}
