package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/proxy"
	"github.com/stubbedev/srv/internal/site"
)

// toJSONMap marshals any value through encoding/json into a map[string]any
// so the MCP SDK's schema reflector doesn't choke on invopop/jsonschema
// struct tags (which use a different syntax than the SDK's reflector
// understands). Returns nil on marshal/unmarshal failure — callers treat
// that as "no metadata available."
func toJSONMap(v any) map[string]any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

// registerReadTools binds every read-only tool to the server.
// Reads never mutate state (no file modifications, no docker calls that
// change container state) so they're always safe to call.
func registerReadTools(srv *mcpsdk.Server) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "version",
		Description: "Return the srv version, commit, and build date. Call first when investigating a bug so the report includes the running build.",
		Annotations: readOnlyAnno("srv version", false),
	}, versionTool)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "paths",
		Description: "Return the on-disk paths srv writes to (config root, sites dir, traefik conf dir, proxies dir). Use this to locate a generated yaml file before reading it.",
		Annotations: readOnlyAnno("srv paths", true),
	}, pathsTool)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "list_sites",
		Description: "List every registered site with name, canonical domain, type (static/dockerfile/compose), is_local flag, and container status. Use this to discover site names before calling get_site, validate_site, or reload_site.",
		Annotations: readOnlyAnno("List sites", true),
	}, listSitesTool)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_site",
		Description: "Return full metadata for one site: domains, aliases, routes, mounts, internal-http flag, network attachments, container status, type, project dir. Pass `name` (site name as listed by list_sites). Use this before any mutation to know the current state.",
		Annotations: readOnlyAnno("Get site", true),
	}, getSiteTool)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "validate_site",
		Description: "Parse a site's metadata.yml and report whether it's valid. Use after manual edits to confirm the daemon's hot-reload will accept the change before saving.",
		Annotations: readOnlyAnno("Validate site metadata", false),
	}, validateSiteTool)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "list_proxies",
		Description: "List every srv-managed proxy by name. Proxies route a domain to a non-Docker upstream (localhost:PORT, container:PORT, or URL). Use to discover proxy names before get_proxy.",
		Annotations: readOnlyAnno("List proxies", false),
	}, listProxiesTool)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_proxy",
		Description: "Return full metadata for one proxy: domains, aliases, wildcard flag, is_local flag, attached routes. Pass `name` as listed by list_proxies.",
		Annotations: readOnlyAnno("Get proxy", false),
	}, getProxyTool)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "list_redirects",
		Description: "List every srv-managed redirect by name. A redirect is either HTTP-layer (301/302 to another URL) or DNS-only (DNS-level alias). Use to discover redirect names before further inspection.",
		Annotations: readOnlyAnno("List redirects", true),
	}, listRedirectsTool)
}

// ─── version + paths ─────────────────────────────────────────────────

type versionIn struct{}
type versionOut struct {
	Version string `json:"version"`
}

func versionTool(_ context.Context, _ *mcpsdk.CallToolRequest, _ versionIn) (*mcpsdk.CallToolResult, versionOut, error) {
	return nil, versionOut{Version: version}, nil
}

type pathsIn struct{}
type pathsOut struct {
	ConfigRoot     string `json:"config_root"`
	SitesDir       string `json:"sites_dir"`
	ProxiesDir     string `json:"proxies_dir"`
	TraefikDir     string `json:"traefik_dir"`
	TraefikConfDir string `json:"traefik_conf_dir"`
	UserConfigFile string `json:"user_config_file"`
}

func pathsTool(_ context.Context, _ *mcpsdk.CallToolRequest, _ pathsIn) (*mcpsdk.CallToolResult, pathsOut, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, pathsOut{}, fmt.Errorf("load config: %w", err)
	}
	return nil, pathsOut{
		ConfigRoot:     cfg.Root,
		SitesDir:       filepath.Join(cfg.Root, "sites"),
		ProxiesDir:     filepath.Join(cfg.Root, "proxies"),
		TraefikDir:     cfg.TraefikDir,
		TraefikConfDir: cfg.TraefikConfDir(),
		UserConfigFile: filepath.Join(cfg.Root, constants.UserConfigFile),
	}, nil
}

// ─── sites ───────────────────────────────────────────────────────────

type siteSummary struct {
	Name    string `json:"name"`
	Domain  string `json:"domain"`
	Type    string `json:"type"`
	IsLocal bool   `json:"is_local"`
	Status  string `json:"status,omitempty"`
	Broken  bool   `json:"broken,omitempty"`
}

type listSitesIn struct{}
type listSitesOut struct {
	Sites []siteSummary `json:"sites"`
}

func listSitesTool(_ context.Context, _ *mcpsdk.CallToolRequest, _ listSitesIn) (*mcpsdk.CallToolResult, listSitesOut, error) {
	sites, err := site.List()
	if err != nil {
		return nil, listSitesOut{}, fmt.Errorf("list sites: %w", err)
	}
	out := make([]siteSummary, 0, len(sites))
	for _, s := range sites {
		out = append(out, siteSummary{
			Name:    s.Name,
			Domain:  s.Domain(),
			Type:    string(s.Type),
			IsLocal: s.IsLocal,
			Status:  s.Status,
			Broken:  s.IsBroken,
		})
	}
	return nil, listSitesOut{Sites: out}, nil
}

type getSiteIn struct {
	Name string `json:"name"`
}
type getSiteOut struct {
	Site     siteSummary    `json:"site"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Dir      string         `json:"dir,omitempty"`
	Domains  []string       `json:"domains,omitempty"`
}

func getSiteTool(_ context.Context, _ *mcpsdk.CallToolRequest, in getSiteIn) (*mcpsdk.CallToolResult, getSiteOut, error) {
	if in.Name == "" {
		return nil, getSiteOut{}, fmt.Errorf("name is required")
	}
	s, err := site.GetByName(in.Name)
	if err != nil {
		return nil, getSiteOut{}, err
	}
	if s == nil {
		return nil, getSiteOut{}, fmt.Errorf("site %q not found", in.Name)
	}
	meta, _ := site.ReadSiteMetadata(in.Name)
	return nil, getSiteOut{
		Site: siteSummary{
			Name:    s.Name,
			Domain:  s.Domain(),
			Type:    string(s.Type),
			IsLocal: s.IsLocal,
			Status:  s.Status,
			Broken:  s.IsBroken,
		},
		Metadata: redactedJSONMap(meta),
		Dir:      s.Dir,
		Domains:  s.Domains,
	}, nil
}

type validateSiteIn struct {
	Name string `json:"name"`
}
type validateSiteOut struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func validateSiteTool(_ context.Context, _ *mcpsdk.CallToolRequest, in validateSiteIn) (*mcpsdk.CallToolResult, validateSiteOut, error) {
	if in.Name == "" {
		return nil, validateSiteOut{}, fmt.Errorf("name is required")
	}
	meta, err := site.ReadSiteMetadata(in.Name)
	if err != nil {
		return nil, validateSiteOut{OK: false, Error: err.Error()}, nil //nolint:nilerr // validation failure reported in payload, not as call error
	}
	if meta == nil {
		return nil, validateSiteOut{OK: false, Error: "site has no metadata.yml"}, nil
	}
	if err := site.ValidateMetadata(meta); err != nil {
		return nil, validateSiteOut{OK: false, Error: err.Error()}, nil //nolint:nilerr // validation failure reported in payload, not as call error
	}
	return nil, validateSiteOut{OK: true}, nil
}

// ─── proxies ─────────────────────────────────────────────────────────

type listProxiesIn struct{}
type listProxiesOut struct {
	Proxies []string `json:"proxies"`
}

func listProxiesTool(_ context.Context, _ *mcpsdk.CallToolRequest, _ listProxiesIn) (*mcpsdk.CallToolResult, listProxiesOut, error) {
	return nil, listProxiesOut{Proxies: proxy.ListNames()}, nil
}

type getProxyIn struct {
	Name string `json:"name"`
}
type getProxyOut struct {
	Proxy map[string]any `json:"proxy"`
}

func getProxyTool(_ context.Context, _ *mcpsdk.CallToolRequest, in getProxyIn) (*mcpsdk.CallToolResult, getProxyOut, error) {
	if in.Name == "" {
		return nil, getProxyOut{}, fmt.Errorf("name is required")
	}
	m, err := proxy.Read(in.Name)
	if err != nil {
		return nil, getProxyOut{}, err
	}
	if m == nil {
		return nil, getProxyOut{}, fmt.Errorf("proxy %q not found", in.Name)
	}
	return nil, getProxyOut{Proxy: redactedJSONMap(m)}, nil
}

// ─── redirects ───────────────────────────────────────────────────────

type listRedirectsIn struct{}
type listRedirectsOut struct {
	Redirects []string `json:"redirects"`
}

func listRedirectsTool(_ context.Context, _ *mcpsdk.CallToolRequest, _ listRedirectsIn) (*mcpsdk.CallToolResult, listRedirectsOut, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, listRedirectsOut{}, fmt.Errorf("load config: %w", err)
	}
	entries, err := os.ReadDir(cfg.TraefikConfDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, listRedirectsOut{Redirects: []string{}}, nil
		}
		return nil, listRedirectsOut{}, fmt.Errorf("read traefik conf dir: %w", err)
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, constants.RedirectConfigPrefix) && strings.HasSuffix(n, constants.ExtYAML) {
			short := strings.TrimSuffix(strings.TrimPrefix(n, constants.RedirectConfigPrefix), constants.ExtYAML)
			names = append(names, short)
		}
	}
	return nil, listRedirectsOut{Redirects: names}, nil
}
