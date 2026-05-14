// Package site — compose.go parses docker-compose.yml files: the ComposeFile
// schema, custom value unmarshalers (labels can be a map or a list, env vars
// the same), service-info extraction (container name, port, profile), and
// the `${VAR:-default}` env interpolation that compose uses for port values.
package site

import (
	"bufio"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

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
