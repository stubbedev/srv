// Package site — php_pool.go wires PHP sites into the shared FPM pool:
// generate the per-site docker-compose.yml (nginx web container only),
// resolve which pool fingerprint the site belongs to, refresh the pool's
// own compose file with every member site, and tear pools down when their
// last member is removed.
package site

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/pool"
)

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
	StampSrvLabels(labels, name, string(meta.Type))

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
	StampSrvLabels(labels, name, string(meta.Type))

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
