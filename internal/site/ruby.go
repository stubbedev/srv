// Package site handles site management operations.
package site

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/traefik"
)

// =============================================================================
// Ruby Site Detection
// =============================================================================

// RubySiteInfo holds detected configuration for a Ruby project.
type RubySiteInfo struct {
	RubyVersion string // "latest", "3.3", etc.
	Framework   string // "rails", "sinatra", "rack", "generic"
	StartCmd    string // e.g. "bundle exec rails server -b 0.0.0.0 -p 3000"
	Port        int    // Container port to proxy to
}

// gemfile represents the minimal fields we care about in a Gemfile.
// We parse it as raw text since Gemfile is Ruby DSL, not a structured format.
type gemfileInfo struct {
	hasRails   bool
	hasSinatra bool
	hasRack    bool
}

// DetectRubySite checks whether dir contains a Ruby project.
// Returns nil if no Gemfile is found.
func DetectRubySite(dir string) (*RubySiteInfo, error) {
	gemfilePath := filepath.Join(dir, "Gemfile")
	data, err := os.ReadFile(gemfilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading Gemfile: %w", err)
	}

	info := parseGemfile(string(data))
	framework := detectRubyFramework(info)
	version := detectRubyVersion(dir)
	port := constants.RubyDefaultPort
	startCmd := buildRubyStartCmd(framework, port)

	return &RubySiteInfo{
		RubyVersion: version,
		Framework:   framework,
		StartCmd:    startCmd,
		Port:        port,
	}, nil
}

// parseGemfile scans a Gemfile for known gem names.
func parseGemfile(content string) gemfileInfo {
	var info gemfileInfo
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, `"rails"`) || strings.Contains(line, `'rails'`) {
			info.hasRails = true
		}
		if strings.Contains(line, `"sinatra"`) || strings.Contains(line, `'sinatra'`) {
			info.hasSinatra = true
		}
		if strings.Contains(line, `"rack"`) || strings.Contains(line, `'rack'`) {
			info.hasRack = true
		}
	}
	return info
}

// detectRubyFramework infers the framework from Gemfile contents.
func detectRubyFramework(info gemfileInfo) string {
	switch {
	case info.hasRails:
		return constants.RubyFrameworkRails
	case info.hasSinatra:
		return constants.RubyFrameworkSinatra
	case info.hasRack:
		return constants.RubyFrameworkRack
	default:
		return constants.RubyFrameworkGeneric
	}
}

// detectRubyVersion reads .ruby-version or returns "latest".
func detectRubyVersion(dir string) string {
	path := filepath.Join(dir, ".ruby-version")
	data, err := os.ReadFile(path)
	if err != nil {
		return constants.RubyVersionLatest
	}
	v := strings.TrimSpace(string(data))
	v = strings.TrimPrefix(v, "ruby-")
	if v == "" {
		return constants.RubyVersionLatest
	}
	// "3.3.0" → "3.3" (keep minor for ruby since patch versions matter more)
	parts := strings.SplitN(v, ".", 3)
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return v
}

// buildRubyStartCmd builds the server start command for a given framework.
func buildRubyStartCmd(framework string, port int) string {
	installCmd := "bundle install"
	switch framework {
	case constants.RubyFrameworkRails:
		return fmt.Sprintf("sh -c '%s && bundle exec rails server -b 0.0.0.0 -p %d'", installCmd, port)
	case constants.RubyFrameworkSinatra:
		return fmt.Sprintf("sh -c '%s && ruby app.rb -o 0.0.0.0 -p %d'", installCmd, port)
	case constants.RubyFrameworkRack:
		return fmt.Sprintf("sh -c '%s && bundle exec rackup --host 0.0.0.0 --port %d'", installCmd, port)
	default:
		return fmt.Sprintf("sh -c '%s && bundle exec ruby app.rb'", installCmd)
	}
}

// RubyImageTag returns the Docker image tag for the given Ruby version.
func RubyImageTag(version string) string {
	if version == "" || version == constants.RubyVersionLatest {
		return constants.RubyImageAlpine
	}
	return fmt.Sprintf(constants.RubyImageFormat, version)
}

// =============================================================================
// Docker Compose generation (Ruby)
// =============================================================================

type rubyServiceConfig struct {
	ContainerName string             `yaml:"container_name"`
	Image         string             `yaml:"image"`
	Command       string             `yaml:"command"`
	WorkingDir    string             `yaml:"working_dir"`
	Volumes       []nodeVolumeConfig `yaml:"volumes"`
	Environment   map[string]string  `yaml:"environment,omitempty"`
	Labels        map[string]string  `yaml:"labels,omitempty"`
	Networks      []string           `yaml:"networks"`
	Restart       string             `yaml:"restart"`
	HealthCheck   *staticHealthCheck `yaml:"healthcheck,omitempty"`
}

type rubyComposeConfig struct {
	Services map[string]rubyServiceConfig `yaml:"services"`
	Networks map[string]nodeNetworkConfig `yaml:"networks"`
}

// WriteRubySiteConfig generates and writes docker-compose.yml for a Ruby site.
func WriteRubySiteConfig(name string, meta SiteMetadata, info *RubySiteInfo, force bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	siteDir := SiteConfigDir(cfg, name)
	if err := os.MkdirAll(siteDir, constants.DirPermDefault); err != nil {
		return fmt.Errorf("failed to create site config directory: %w", err)
	}

	containerName := "srv-" + name + "-app"
	image := RubyImageTag(info.RubyVersion)
	labels := buildAppTraefikLabels(name, meta.Domains, meta.IsLocal, meta.Wildcard, info.Port)
	if HasListener(meta.Listeners, constants.ListenerInternal) {
		addInternalListenerLabels(labels, name, meta.Domains, meta.Wildcard)
	}

	composeConfig := rubyComposeConfig{
		Services: map[string]rubyServiceConfig{
			"app": {
				ContainerName: containerName,
				Image:         image,
				Command:       info.StartCmd,
				WorkingDir:    constants.RubyDockerWorkDir,
				Volumes: []nodeVolumeConfig{
					{
						Type:        "bind",
						Source:      meta.ProjectPath,
						Target:      constants.RubyDockerWorkDir,
						Consistency: volumeConsistencyForHost(),
					},
				},
				Environment: map[string]string{
					"PORT":      fmt.Sprintf("%d", info.Port),
					"RACK_ENV":  "development",
					"RAILS_ENV": "development",
				},
				Labels:      labels,
				Networks:    []string{constants.TraefikSubdir},
				Restart:     constants.RestartUnlessStopped,
				HealthCheck: makeStaticHealthCheck(info.Port),
			},
		},
		Networks: map[string]nodeNetworkConfig{
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

	header := fmt.Sprintf(`# Generated by srv - ruby site (%s)
# Project: %s
#
# This file is yours to edit. Changes take effect on next restart.
#
# Common customisations:
#   environment:
#     RAILS_ENV: "production"   # Change environment
#     PORT: "%d"               # Override listen port

`, info.Framework, meta.ProjectPath, info.Port)
	content := header + string(data)

	composePath := SiteComposePath(cfg, name)
	return writeFile(composePath, []byte(content), force)
}

// buildAppTraefikLabels builds Traefik labels for app container sites (Ruby, Python, Dockerfile).
func buildAppTraefikLabels(name string, domains []string, isLocal, wildcard bool, port int) map[string]string {
	labels := map[string]string{
		"traefik.enable": "true",
		fmt.Sprintf("traefik.http.routers.%s.rule", name):                      traefik.BuildHostRule(domains, wildcard),
		fmt.Sprintf("traefik.http.routers.%s.entrypoints", name):               "websecure",
		fmt.Sprintf("traefik.http.routers.%s.tls", name):                       "true",
		fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", name): fmt.Sprintf("%d", port),
	}
	if !isLocal {
		labels[fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", name)] = "letsencrypt"
	}
	return labels
}
