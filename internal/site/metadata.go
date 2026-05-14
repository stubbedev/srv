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
	MaxBody        string `yaml:"max_body,omitempty" jsonschema:"description=Maximum request body size (e.g. '2G' '128M' '500k')."`
	ReadTimeout    string `yaml:"read_timeout,omitempty" jsonschema:"description=Read timeout (e.g. '300s' '5m')."`
	SendTimeout    string `yaml:"send_timeout,omitempty" jsonschema:"description=Send timeout (e.g. '300s')."`
	ConnectTimeout string `yaml:"connect_timeout,omitempty" jsonschema:"description=Upstream connect timeout (e.g. '5s')."`
}

// Upstream points a route or proxy at a backend. Exactly one of Port/Container/URL
// is set per Kind.
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
	Limits           *Limits  `yaml:"limits,omitempty" jsonschema:"description=Per-route limits override."`
}

// Fallback configures a remote upstream that takes over when the primary
// upstream returns a 5xx response. Implemented via a small nginx sidecar in
// front of the main upstream.
type Fallback struct {
	URL     string `yaml:"url" jsonschema:"description=Remote URL to fall back to on 5xx (e.g. https://prod.example.com)."`
	Timeout string `yaml:"timeout,omitempty" jsonschema:"description=Fallback request timeout (e.g. '2s')."`
}

// CurrentMetadataSchema is the version written to new metadata.yml files. Bump
// when introducing a breaking, non-additive change.
const CurrentMetadataSchema = 1

// SiteMetadata holds all configuration for a site.
// This is stored in ~/.config/srv/sites/{name}/metadata.yml
type SiteMetadata struct {
	SchemaVersion      int       `yaml:"schema_version,omitempty" jsonschema:"description=metadata.yml schema version (1 = current)."`
	Type               SiteType  `yaml:"type" jsonschema:"enum=compose,enum=static,enum=php,enum=node,enum=ruby,enum=python,enum=dockerfile,description=Site runtime type."`
	Domains            []string  `yaml:"domains,omitempty" jsonschema:"description=All hostnames; the first entry is canonical."`
	ProjectPath        string    `yaml:"project_path" jsonschema:"description=Absolute path to the project on disk."`
	ServiceName        string    `yaml:"service_name,omitempty" jsonschema:"description=Container name used for Traefik routing."`
	ComposeServiceName string    `yaml:"compose_service_name,omitempty" jsonschema:"description=docker-compose service name (for compose commands)."`
	Profile            string    `yaml:"profile,omitempty" jsonschema:"description=docker-compose profile (if the service uses profiles)."`
	Port               int       `yaml:"port" jsonschema:"description=Port the service listens on inside the container."`
	IsLocal            bool      `yaml:"is_local" jsonschema:"description=Whether to use a locally-issued (mkcert) SSL certificate."`
	Wildcard           bool      `yaml:"wildcard,omitempty" jsonschema:"description=Match apex + one-level subdomains (*.example.com)."`
	NetworkName        string    `yaml:"network_name" jsonschema:"description=Docker network the site joins."`
	Listeners          []string  `yaml:"listeners,omitempty" jsonschema:"description=Extra Traefik entrypoints (e.g. 'internal' for plain HTTP on :88)."`
	Limits             *Limits   `yaml:"limits,omitempty" jsonschema:"description=Request-body / timeout overrides."`
	Routes             []Route   `yaml:"routes,omitempty" jsonschema:"description=Extra Traefik routers (path-prefix / regex-rewrite splits)."`
	Upstream           *Upstream `yaml:"upstream,omitempty" jsonschema:"description=Primary backend (proxy-type sites only)."`
	Fallback           *Fallback `yaml:"fallback,omitempty" jsonschema:"description=Remote 5xx fallback target."`
	// Static site options
	SPA   bool `yaml:"spa,omitempty" jsonschema:"description=Single-page-app mode (fall back to /index.html)."`
	Cache bool `yaml:"cache,omitempty" jsonschema:"description=Emit aggressive caching headers for static assets."`
	CORS  bool `yaml:"cors,omitempty" jsonschema:"description=Emit permissive CORS headers."`
	// PHP site options
	PHPVersion    string   `yaml:"php_version,omitempty" jsonschema:"description=PHP version ('latest' or '8.3')."`
	PHPExtensions []string `yaml:"php_extensions,omitempty" jsonschema:"description=Extra PHP extensions to install."`
	PHPFramework  string   `yaml:"php_framework,omitempty" jsonschema:"description=Detected PHP framework."`
	DocumentRoot  string   `yaml:"document_root,omitempty" jsonschema:"description=Document root relative to the project (e.g. 'public')."`
	// Node.js / Bun / Deno site options
	NodeRuntime        string `yaml:"node_runtime,omitempty" jsonschema:"enum=node,enum=bun,enum=deno,description=JavaScript runtime."`
	NodePackageManager string `yaml:"node_package_manager,omitempty" jsonschema:"enum=npm,enum=yarn,enum=pnpm,enum=bun,enum=deno,description=Package manager."`
	NodeVersion        string `yaml:"node_version,omitempty" jsonschema:"description=Node version ('lts' or '20'; node runtime only)."`
	NodeFramework      string `yaml:"node_framework,omitempty" jsonschema:"description=Detected Node framework."`
	NodeStartCmd       string `yaml:"node_start_cmd,omitempty" jsonschema:"description=Start command (e.g. 'npm run dev')."`
	// Ruby site options
	RubyVersion   string `yaml:"ruby_version,omitempty" jsonschema:"description=Ruby version ('latest' or '3.3')."`
	RubyFramework string `yaml:"ruby_framework,omitempty" jsonschema:"enum=rails,enum=sinatra,enum=rack,enum=generic,description=Detected Ruby framework."`
	RubyStartCmd  string `yaml:"ruby_start_cmd,omitempty" jsonschema:"description=Start command."`
	// Python site options
	PythonVersion   string `yaml:"python_version,omitempty" jsonschema:"description=Python version ('latest' or '3.12')."`
	PythonFramework string `yaml:"python_framework,omitempty" jsonschema:"enum=django,enum=fastapi,enum=flask,enum=generic,description=Detected Python framework."`
	PythonStartCmd  string `yaml:"python_start_cmd,omitempty" jsonschema:"description=Start command."`
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
