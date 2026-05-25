// Package site — php_franken.go owns the PHP runtime artifacts: a per-site
// Dockerfile built FROM dunglas/frankenphp:php<version>-alpine plus
// install-php-extensions, and a docker-compose.yml that runs that image as one
// container per site. No nginx shim, no separate FPM container, no shared
// pool — FrankenPHP embeds PHP in Caddy and exposes HTTP directly. Traefik
// terminates TLS and labels on the FrankenPHP container do all the routing.
package site

import (
	"fmt"
	"os"
	"runtime"

	"gopkg.in/yaml.v3"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
)

// =============================================================================
// Compose schema (shared with other site types in this package)
// =============================================================================

// phpVolumeConfig is a bind-mount volume entry.
type phpVolumeConfig struct {
	Type        string `yaml:"type"`
	Source      string `yaml:"source"`
	Target      string `yaml:"target"`
	ReadOnly    bool   `yaml:"read_only,omitempty"`
	Consistency string `yaml:"consistency,omitempty"` // "cached" on macOS — cuts inode roundtrips
}

// phpBuildConfig is the build context for the FrankenPHP service.
type phpBuildConfig struct {
	Context    string `yaml:"context"`
	Dockerfile string `yaml:"dockerfile"`
}

// phpServiceConfig is one service entry in a generated docker-compose.yml.
type phpServiceConfig struct {
	Build         *phpBuildConfig   `yaml:"build,omitempty"`
	ContainerName string            `yaml:"container_name"`
	Image         string            `yaml:"image,omitempty"`
	PullPolicy    string            `yaml:"pull_policy,omitempty"`
	User          string            `yaml:"user,omitempty"`
	Environment   map[string]string `yaml:"environment,omitempty"`
	Volumes       []phpVolumeConfig `yaml:"volumes,omitempty"`
	Labels        map[string]string `yaml:"labels,omitempty"`
	Networks      []string          `yaml:"networks"`
	ExtraHosts    []string          `yaml:"extra_hosts,omitempty"`
	Restart       string            `yaml:"restart"`
	DependsOn     []string          `yaml:"depends_on,omitempty"`
	HealthCheck   *healthCheck      `yaml:"healthcheck,omitempty"`
}

// healthCheck is a compose-format healthcheck spec.
type healthCheck struct {
	Test        []string `yaml:"test"`
	Interval    string   `yaml:"interval,omitempty"`
	Timeout     string   `yaml:"timeout,omitempty"`
	StartPeriod string   `yaml:"start_period,omitempty"`
	Retries     int      `yaml:"retries,omitempty"`
}

// makeHealthCheck builds a cheap TCP-probe healthcheck for the given port.
// Uses busybox `nc -z` which is present in alpine images.
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

// =============================================================================
// Container / image naming
// =============================================================================

// PHPContainerName returns the FrankenPHP container name for the given site:
// "srv-<name>-php". Exported because shell / shell-completion code needs it.
func PHPContainerName(siteName string) string {
	return fmt.Sprintf(constants.FrankenPHPContainerNameFormat, siteName)
}

// PHPImageTag returns the per-site image tag srv builds: "srv-php-<name>:latest".
func PHPImageTag(siteName string) string {
	return fmt.Sprintf(constants.FrankenPHPImageTagFormat, siteName)
}

// FrankenPHPBaseImage returns the upstream image tag for a given PHP version
// constraint. "" or "latest" → `dunglas/frankenphp:alpine`; otherwise
// `dunglas/frankenphp:php<version>-alpine`.
func FrankenPHPBaseImage(version string) string {
	if version == "" || version == constants.PHPVersionLatest {
		return constants.FrankenPHPImageLatest
	}
	return fmt.Sprintf(constants.FrankenPHPImageFormat, version)
}

// =============================================================================
// File generation
// =============================================================================

// WritePHPSiteConfig generates the Dockerfile and docker-compose.yml for a PHP
// site into the srv config directory under ~/.config/srv/sites/<name>/.
//
// If the project has a Dockerfile.srv (or .srv/Dockerfile) at its root the
// contents are copied verbatim instead of generating the default template.
// The override must FROM dunglas/frankenphp:... so srv's runtime assumptions
// (HTTP on :80, Caddy + embedded PHP) still hold.
//
// If force is false, existing files are left untouched so user edits are
// preserved.
func WritePHPSiteConfig(name string, meta SiteMetadata, info *PHPSiteInfo, force bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	siteDir := SiteConfigDir(cfg, name)
	if err := os.MkdirAll(siteDir, constants.DirPermDefault); err != nil {
		return fmt.Errorf("failed to create site config directory: %w", err)
	}

	dockerfile, _, err := resolvePHPDockerfile(meta, info)
	if err != nil {
		return err
	}
	if err := writeFile(SitePHPDockerfilePath(cfg, name), []byte(dockerfile), force); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}

	composeYAML, err := renderPHPCompose(name, meta, info, siteDir)
	if err != nil {
		return fmt.Errorf("render compose: %w", err)
	}
	return writeFile(SiteComposePath(cfg, name), composeYAML, force)
}

// resolvePHPDockerfile returns the Dockerfile contents to write for a PHP
// site. It checks the project root for `Dockerfile.srv` then `.srv/Dockerfile`;
// the first match wins and is returned verbatim after validating the FROM
// line. The second return value is the source path of the override (empty
// when no override was used).
func resolvePHPDockerfile(meta SiteMetadata, info *PHPSiteInfo) (string, string, error) {
	candidates := []string{
		meta.ProjectPath + "/Dockerfile.srv",
		meta.ProjectPath + "/.srv/Dockerfile",
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path) //nolint:gosec // path derived from site metadata
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", "", fmt.Errorf("read %s: %w", path, err)
		}
		if err := validateFrankenPHPBaseImage(data, path); err != nil {
			return "", "", err
		}
		return string(data), path, nil
	}
	return generatePHPDockerfile(info), "", nil
}

// validateFrankenPHPBaseImage scans a Dockerfile for the first uncommented
// FROM instruction and confirms it points at dunglas/frankenphp. srv's
// runtime expectations (HTTP on :80, Caddy + embedded PHP) only hold for
// that image family.
func validateFrankenPHPBaseImage(data []byte, path string) error {
	for line := range splitLines(string(data)) {
		t := trimSpaceASCII(line)
		if t == "" || t[0] == '#' {
			continue
		}
		// Match "FROM image[:tag]" case-insensitively.
		if len(t) < 5 || (t[:5] != "FROM " && t[:5] != "from " && t[:5] != "From ") {
			continue
		}
		rest := trimSpaceASCII(t[5:])
		if !startsWith(rest, "dunglas/frankenphp") {
			return fmt.Errorf("%s: FROM must be dunglas/frankenphp[:tag], got %q", path, rest)
		}
		return nil
	}
	return fmt.Errorf("%s: no FROM instruction found", path)
}

// splitLines yields each line of s without allocating a slice.
func splitLines(s string) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		start := 0
		for i := 0; i < len(s); i++ {
			if s[i] == '\n' {
				if !yield(s[start:i]) {
					return
				}
				start = i + 1
			}
		}
		if start < len(s) {
			yield(s[start:])
		}
	}
}

func trimSpaceASCII(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// WritePHPDockerConfig regenerates the Dockerfile + docker-compose.yml after a
// PHP version or extension change. Always force-overwrites the generated files.
// Called from `srv runtime` after metadata is updated.
func WritePHPDockerConfig(name string, meta SiteMetadata, info *PHPSiteInfo) error {
	return WritePHPSiteConfig(name, meta, info, true)
}

// SitePHPDockerfilePath returns the path to the per-site Dockerfile.
func SitePHPDockerfilePath(cfg *config.Config, name string) string {
	return SiteConfigDir(cfg, name) + "/" + constants.PHPDockerfileFile
}

// renderPHPCompose builds the docker-compose.yml content for a PHP site.
// Separate from WritePHPSiteConfig so tests can exercise it without touching the
// filesystem.
func renderPHPCompose(name string, meta SiteMetadata, info *PHPSiteInfo, siteDir string) ([]byte, error) {
	containerName := PHPContainerName(name)
	imageTag := PHPImageTag(name)

	labels := buildStaticTraefikLabels(name, meta.Domains, meta.IsLocal, meta.Wildcard)
	if HasListener(meta.Listeners, constants.ListenerInternal) {
		addInternalListenerLabels(labels, name, meta.Domains, meta.Wildcard)
	}
	StampSrvLabels(labels, name, string(meta.Type))
	// FrankenPHP listens on port 80 inside the container. Override the default
	// loadbalancer port (which the static label builder leaves at 80 already,
	// but pin it explicitly so future changes to the helper don't break PHP).
	labels[fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", name)] = fmt.Sprintf("%d", constants.FrankenPHPContainerPort)

	docRoot := constants.FrankenPHPAppDir
	if info.DocumentRoot != "" {
		docRoot = constants.FrankenPHPAppDir + "/" + info.DocumentRoot
	}

	env := map[string]string{
		// FrankenPHP listens HTTP-only inside the container; Traefik handles TLS.
		"SERVER_NAME": fmt.Sprintf(":%d", constants.FrankenPHPContainerPort),
		"SERVER_ROOT": docRoot,
		// Disable Caddy's automatic HTTPS — Traefik terminates TLS upstream.
		"CADDY_GLOBAL_OPTIONS": "auto_https off",
	}

	volumes := []phpVolumeConfig{
		{
			Type:        "bind",
			Source:      meta.ProjectPath,
			Target:      constants.FrankenPHPAppDir,
			Consistency: volumeConsistencyForHost(),
		},
	}
	for _, v := range meta.Volumes {
		volumes = append(volumes, phpVolumeConfig{
			Type:     "bind",
			Source:   v.Source,
			Target:   v.Target,
			ReadOnly: v.ReadOnly,
		})
	}

	serviceNetworks := append([]string{constants.TraefikSubdir}, meta.ExtraNetworks...)
	service := phpServiceConfig{
		Build: &phpBuildConfig{
			Context:    siteDir,
			Dockerfile: constants.PHPDockerfileFile,
		},
		Image:         imageTag,
		PullPolicy:    "missing",
		ContainerName: containerName,
		User:          hostUserSpec(),
		Environment:   env,
		Volumes:       volumes,
		Labels:        labels,
		Networks:      serviceNetworks,
		ExtraHosts:    linuxExtraHosts(),
		Restart:       constants.RestartUnlessStopped,
		HealthCheck:   makeHealthCheck(constants.FrankenPHPContainerPort),
	}

	networks := map[string]phpNetworkConfig{
		constants.TraefikSubdir: {
			Name:     meta.NetworkName,
			External: true,
		},
	}
	for _, n := range meta.ExtraNetworks {
		networks[n] = phpNetworkConfig{Name: n, External: true}
	}

	compose := phpComposeConfig{
		Name:     constants.ComposeProjectName,
		Services: map[string]phpServiceConfig{constants.FrankenPHPServiceName: service},
		Networks: networks,
	}

	data, err := yaml.Marshal(&compose)
	if err != nil {
		return nil, fmt.Errorf("marshal compose: %w", err)
	}

	header := fmt.Sprintf(`# Generated by srv - PHP site (%s)
# Project: %s
#
# Runtime: FrankenPHP (Caddy + embedded PHP). One container per site.
# This file is yours to edit. Changes take effect on next restart.

`, info.Framework, meta.ProjectPath)
	return append([]byte(header), data...), nil
}

// hostUserSpec returns the `<uid>:<gid>` string used for the compose `user:`
// field so files written by PHP inside the container are owned by the host
// user. On Windows there is no uid/gid model — return empty so Compose omits
// the field.
func hostUserSpec() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	return fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
}

// linuxExtraHosts returns the extra_hosts entries to add to the FrankenPHP
// container so PHP code can reach services on host loopback via
// `host.docker.internal`. On macOS / Windows Docker Desktop already provides
// this name natively so we add nothing.
func linuxExtraHosts() []string {
	if runtime.GOOS != "linux" {
		return nil
	}
	return []string{"host.docker.internal:host-gateway"}
}

// =============================================================================
// Small filesystem helpers shared by site detection / generators
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
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}
