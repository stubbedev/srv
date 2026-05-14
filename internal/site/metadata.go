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
	MaxBody        string `yaml:"max_body,omitempty"`     // e.g. "2G", "128M"
	ReadTimeout    string `yaml:"read_timeout,omitempty"` // e.g. "300s"
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
	ID               string   `yaml:"id"`                   // stable handle for CLI
	Path             string   `yaml:"path,omitempty"`       // PathPrefix
	PathRegex        string   `yaml:"path_regex,omitempty"` // PathRegexp
	Rewrite          string   `yaml:"rewrite,omitempty"`    // ReplacePathRegex replacement
	Upstream         Upstream `yaml:"upstream"`
	PreserveHost     *bool    `yaml:"preserve_host,omitempty"` // tri-state; nil = default true
	PassRangeHeaders bool     `yaml:"pass_range_headers,omitempty"`
	Priority         int      `yaml:"priority,omitempty"` // optional Traefik priority override
	Limits           *Limits  `yaml:"limits,omitempty"`   // per-route override
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
