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
	"github.com/stubbedev/srv/internal/fsutil"
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
	Kind      string `jsonschema:"enum=localhost,enum=container,enum=url,description=Upstream target type." yaml:"kind"`
	Port      int    `jsonschema:"description=Port when kind=localhost or kind=container."                  yaml:"port,omitempty"`
	Container string `jsonschema:"description=Container name when kind=container."                          yaml:"container,omitempty"`
	URL       string `jsonschema:"description=Full URL when kind=url (e.g. https://api.example.com)."       yaml:"url,omitempty"`
	// InsecureSkipVerify disables TLS cert verification for an https url upstream
	// whose certificate can't be verified (self-signed, or a SAN that doesn't
	// match the dialed host/IP). No effect on http upstreams.
	InsecureSkipVerify bool `jsonschema:"description=Skip TLS verification for an https url upstream (self-signed / mismatched cert)." yaml:"insecure_skip_verify,omitempty"`
}

// Route attaches an extra Traefik router to a site, used for path-prefix splits
// (e.g. /app → WebSocket on :6001) or regex rewrites (e.g. /videos/...).
type Route struct {
	ID               string   `jsonschema:"description=Stable handle for the route used by 'srv route' CLI."    yaml:"id"`
	Path             string   `jsonschema:"description=PathPrefix to match (e.g. /api)."                        yaml:"path,omitempty"`
	PathRegex        string   `jsonschema:"description=Regex pattern to match (Traefik PathRegexp)."            yaml:"path_regex,omitempty"`
	Rewrite          string   `jsonschema:"description=ReplacePathRegex replacement (e.g. /v1/$1)."             yaml:"rewrite,omitempty"`
	Upstream         Upstream `yaml:"upstream"`
	PreserveHost     *bool    `jsonschema:"description=Whether to preserve the Host header (default true)."     yaml:"preserve_host,omitempty"`
	PassRangeHeaders bool     `jsonschema:"description=Forward Range/If-Range headers for byte-range requests." yaml:"pass_range_headers,omitempty"`
	Priority         int      `jsonschema:"description=Traefik router priority override."                       yaml:"priority,omitempty"`
}

// VolumeMount is an extra bind-mount the user added to a site so its container
// can reach host paths beyond the project root (TEMP dirs, nix-profile
// binaries, demo asset trees, etc.). Source and Target are absolute paths;
// the source must already exist on the host.
type VolumeMount struct {
	Source   string `jsonschema:"description=Absolute host path."                 yaml:"source"`
	Target   string `jsonschema:"description=Absolute path inside the container." yaml:"target"`
	ReadOnly bool   `jsonschema:"description=Mount the bind read-only."           yaml:"read_only,omitempty"`
}

// CurrentMetadataSchema is the version written to new metadata.yml files. Bump
// when introducing a breaking, non-additive change.
const CurrentMetadataSchema = 1

// SiteMetadata holds all configuration for a site.
// This is stored in ~/.config/srv/sites/{name}/metadata.yml.
type SiteMetadata struct {
	SchemaVersion      int           `jsonschema:"description=metadata.yml schema version (1 = current)."                                                         yaml:"schema_version,omitempty"`
	Type               SiteType      `jsonschema:"enum=compose,enum=static,enum=dockerfile,description=Site runtime type."                                        yaml:"type"`
	Domains            []string      `jsonschema:"description=All hostnames; the first entry is canonical."                                                       yaml:"domains,omitempty"`
	ProjectPath        string        `jsonschema:"description=Absolute path to the project on disk."                                                              yaml:"project_path"`
	ServiceName        string        `jsonschema:"description=Container name used for Traefik routing."                                                           yaml:"service_name,omitempty"`
	ComposeServiceName string        `jsonschema:"description=docker-compose service name (for compose commands)."                                                yaml:"compose_service_name,omitempty"`
	Profile            string        `jsonschema:"description=docker-compose profile (if the service uses profiles)."                                             yaml:"profile,omitempty"`
	Port               int           `jsonschema:"description=Port the service listens on inside the container."                                                  yaml:"port"`
	IsLocal            bool          `jsonschema:"description=Whether to use a locally-issued (mkcert) SSL certificate."                                          yaml:"is_local"`
	Wildcard           bool          `jsonschema:"description=Match apex + one-level subdomains (*.example.com)."                                                 yaml:"wildcard,omitempty"`
	NetworkName        string        `jsonschema:"description=Docker network the site joins."                                                                     yaml:"network_name"`
	ExtraNetworks      []string      `jsonschema:"description=Extra external Docker networks the site joins (for reaching user-managed containers like mysql01)." yaml:"extra_networks,omitempty"`
	Volumes            []VolumeMount `jsonschema:"description=Extra host bind-mounts attached to the site's container (e.g. ~/.nix-profile, TEMP dirs)."          yaml:"volumes,omitempty"`
	Listeners          []string      `jsonschema:"description=Extra Traefik entrypoints (e.g. 'internal' for plain HTTP on :88)."                                 yaml:"listeners,omitempty"`
	Routes             []Route       `jsonschema:"description=Extra Traefik routers (path-prefix / regex-rewrite splits)."                                        yaml:"routes,omitempty"`
	// Static site options
	SPA   bool `jsonschema:"description=Single-page-app mode (fall back to /index.html)."   yaml:"spa,omitempty"`
	Cache bool `jsonschema:"description=Emit aggressive caching headers for static assets." yaml:"cache,omitempty"`
	CORS  bool `jsonschema:"description=Emit permissive CORS headers."                      yaml:"cors,omitempty"`
	// Dockerfile site options
	DockerfilePort int `jsonschema:"description=Port discovered from the Dockerfile EXPOSE directive." yaml:"dockerfile_port,omitempty"`
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

	return fsutil.AtomicWriteFile(metadataPath(cfg, name), []byte(content), constants.FilePermDefault)
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
