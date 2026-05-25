// Package site — metadata.go defines SiteMetadata, the on-disk YAML schema
// that records everything srv needs to re-derive a site's runtime config
// (Dockerfile, nginx.conf, Traefik labels, DNS, certs) without re-detecting
// from the project source. Read/write helpers here are the only entry points
// for that file.
package site

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
)

// =============================================================================
// Site Metadata (stored in ~/.config/srv/sites/{name}/)
// =============================================================================

// SiteType represents the type of site being served.
type SiteType string

const (
	SiteTypeCompose    SiteType = constants.SiteTypeCompose    // Docker compose project (user-owned)
	SiteTypeStatic     SiteType = constants.SiteTypeStatic     // Static files served via nginx
	SiteTypeDockerfile SiteType = constants.SiteTypeDockerfile // Dockerfile site (user-owned Dockerfile)
)

// Upstream points a route at a backend. Exactly one of Port/Container/URL is
// set per Kind.
type Upstream struct {
	Kind      string `yaml:"kind" jsonschema:"enum=localhost,enum=container,enum=url,description=Upstream target type."`
	Port      int    `yaml:"port,omitempty" jsonschema:"description=Port when kind=localhost or kind=container."`
	Container string `yaml:"container,omitempty" jsonschema:"description=Container name when kind=container."`
	URL       string `yaml:"url,omitempty" jsonschema:"description=Full URL when kind=url (e.g. https://api.example.com)."`
}

// Route attaches an extra Traefik router to a site, used for path-prefix splits
// (e.g. /app → WebSocket on :6001) or regex rewrites (e.g. /videos/...).
type Route struct {
	ID               string   `yaml:"id" jsonschema:"description=Stable handle for the route used by 'srv route' CLI."`
	Path             string   `yaml:"path,omitempty" jsonschema:"description=PathPrefix to match (e.g. /api)."`
	PathRegex        string   `yaml:"path_regex,omitempty" jsonschema:"description=Regex pattern to match (Traefik PathRegexp)."`
	Rewrite          string   `yaml:"rewrite,omitempty" jsonschema:"description=ReplacePathRegex replacement (e.g. /v1/$1)."`
	Upstream         Upstream `yaml:"upstream"`
	PreserveHost     *bool    `yaml:"preserve_host,omitempty" jsonschema:"description=Whether to preserve the Host header (default true)."`
	PassRangeHeaders bool     `yaml:"pass_range_headers,omitempty" jsonschema:"description=Forward Range/If-Range headers for byte-range requests."`
	Priority         int      `yaml:"priority,omitempty" jsonschema:"description=Traefik router priority override."`
}

// VolumeMount is an extra bind-mount the user added to a site so its container
// can reach host paths beyond the project root (TEMP dirs, nix-profile
// binaries, demo asset trees, etc.). Source and Target are absolute paths;
// the source must already exist on the host.
type VolumeMount struct {
	Source   string `yaml:"source" jsonschema:"description=Absolute host path."`
	Target   string `yaml:"target" jsonschema:"description=Absolute path inside the container."`
	ReadOnly bool   `yaml:"read_only,omitempty" jsonschema:"description=Mount the bind read-only."`
}

// CurrentMetadataSchema is the version written to new metadata.yml files. Bump
// when introducing a breaking, non-additive change.
const CurrentMetadataSchema = 1

// SiteMetadata holds all configuration for a site.
// This is stored in ~/.config/srv/sites/{name}/metadata.yml
type SiteMetadata struct {
	SchemaVersion      int           `yaml:"schema_version,omitempty" jsonschema:"description=metadata.yml schema version (1 = current)."`
	Type               SiteType      `yaml:"type" jsonschema:"enum=compose,enum=static,enum=dockerfile,description=Site runtime type."`
	Domains            []string      `yaml:"domains,omitempty" jsonschema:"description=All hostnames; the first entry is canonical."`
	ProjectPath        string        `yaml:"project_path" jsonschema:"description=Absolute path to the project on disk."`
	ServiceName        string        `yaml:"service_name,omitempty" jsonschema:"description=Container name used for Traefik routing."`
	ComposeServiceName string        `yaml:"compose_service_name,omitempty" jsonschema:"description=docker-compose service name (for compose commands)."`
	Profile            string        `yaml:"profile,omitempty" jsonschema:"description=docker-compose profile (if the service uses profiles)."`
	Port               int           `yaml:"port" jsonschema:"description=Port the service listens on inside the container."`
	IsLocal            bool          `yaml:"is_local" jsonschema:"description=Whether to use a locally-issued (mkcert) SSL certificate."`
	Wildcard           bool          `yaml:"wildcard,omitempty" jsonschema:"description=Match apex + one-level subdomains (*.example.com)."`
	NetworkName        string        `yaml:"network_name" jsonschema:"description=Docker network the site joins."`
	ExtraNetworks      []string      `yaml:"extra_networks,omitempty" jsonschema:"description=Extra external Docker networks the site joins (for reaching user-managed containers like mysql01)."`
	Volumes            []VolumeMount `yaml:"volumes,omitempty" jsonschema:"description=Extra host bind-mounts attached to the site's container (e.g. ~/.nix-profile, TEMP dirs)."`
	Listeners          []string      `yaml:"listeners,omitempty" jsonschema:"description=Extra Traefik entrypoints (e.g. 'internal' for plain HTTP on :88)."`
	Routes             []Route       `yaml:"routes,omitempty" jsonschema:"description=Extra Traefik routers (path-prefix / regex-rewrite splits)."`
	// Static site options
	SPA   bool `yaml:"spa,omitempty" jsonschema:"description=Single-page-app mode (fall back to /index.html)."`
	Cache bool `yaml:"cache,omitempty" jsonschema:"description=Emit aggressive caching headers for static assets."`
	CORS  bool `yaml:"cors,omitempty" jsonschema:"description=Emit permissive CORS headers."`
	// Dockerfile site options
	DockerfilePort int `yaml:"dockerfile_port,omitempty" jsonschema:"description=Port discovered from the Dockerfile EXPOSE directive."`
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

	header := "# yaml-language-server: $schema=" + constants.MetadataSchemaURL + "\n" +
		"# Site metadata - generated by srv\n"
	content := header + string(data)

	return atomicWriteFile(metadataPath(cfg, name), []byte(content), constants.FilePermDefault)
}

// atomicWriteFile writes data to a path via a temp file + rename so a crash
// mid-write cannot leave a half-written metadata.yml on disk.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + constants.ExtTmp
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
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
