package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/proxy"
	"github.com/stubbedev/srv/internal/site"
)

// withRoot points srv state at a fresh temp dir for the duration of the
// test and clears the config cache so the override takes effect. The MCP
// tool handlers all resolve paths through config.Load(), so isolating
// SRV_ROOT is enough to drive them against a clean, throwaway state tree.
//
// These tests run without t.Parallel: SRV_ROOT + config's process-global
// cache are shared mutable state, so concurrent roots would race.
func withRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv(constants.EnvSrvRoot, root)
	config.ResetCache()
	t.Cleanup(config.ResetCache)
	return root
}

func TestVersionTool(t *testing.T) {
	prev := version
	t.Cleanup(func() { version = prev })
	version = "v1.2.3-test"

	_, out, err := versionTool(context.Background(), nil, versionIn{})
	if err != nil {
		t.Fatalf("versionTool: %v", err)
	}
	if out.Version != "v1.2.3-test" {
		t.Errorf("Version = %q, want %q", out.Version, "v1.2.3-test")
	}
}

func TestPathsTool(t *testing.T) {
	root := withRoot(t)

	_, out, err := pathsTool(context.Background(), nil, pathsIn{})
	if err != nil {
		t.Fatalf("pathsTool: %v", err)
	}
	if out.ConfigRoot != root {
		t.Errorf("ConfigRoot = %q, want %q", out.ConfigRoot, root)
	}
	if out.SitesDir != filepath.Join(root, "sites") {
		t.Errorf("SitesDir = %q, want %q", out.SitesDir, filepath.Join(root, "sites"))
	}
	if out.TraefikConfDir != filepath.Join(root, "traefik", "conf") {
		t.Errorf("TraefikConfDir = %q", out.TraefikConfDir)
	}
}

func TestListSitesTool(t *testing.T) {
	withRoot(t)

	// Empty state: no sites dir yet → empty list, no error.
	_, out, err := listSitesTool(context.Background(), nil, listSitesIn{})
	if err != nil {
		t.Fatalf("listSitesTool (empty): %v", err)
	}
	if len(out.Sites) != 0 {
		t.Fatalf("expected 0 sites, got %d", len(out.Sites))
	}

	// Register a static site, then expect it back.
	if err := site.WriteSiteMetadata("demo", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"demo.test"},
		ProjectPath: t.TempDir(),
		IsLocal:     true,
	}); err != nil {
		t.Fatalf("WriteSiteMetadata: %v", err)
	}

	_, out, err = listSitesTool(context.Background(), nil, listSitesIn{})
	if err != nil {
		t.Fatalf("listSitesTool: %v", err)
	}
	if len(out.Sites) != 1 {
		t.Fatalf("expected 1 site, got %d: %+v", len(out.Sites), out.Sites)
	}
	got := out.Sites[0]
	if got.Name != "demo" || got.Domain != "demo.test" || got.Type != string(site.SiteTypeStatic) {
		t.Errorf("site summary mismatch: %+v", got)
	}
}

func TestGetSiteTool(t *testing.T) {
	withRoot(t)

	// Missing name is a request error, not a payload error.
	if _, _, err := getSiteTool(context.Background(), nil, getSiteIn{}); err == nil {
		t.Error("expected error for empty name")
	}

	// Unknown site surfaces an error.
	if _, _, err := getSiteTool(context.Background(), nil, getSiteIn{Name: "nope"}); err == nil {
		t.Error("expected error for unknown site")
	}

	if err := site.WriteSiteMetadata("demo", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"demo.test", "alias.test"},
		ProjectPath: t.TempDir(),
	}); err != nil {
		t.Fatalf("WriteSiteMetadata: %v", err)
	}

	_, out, err := getSiteTool(context.Background(), nil, getSiteIn{Name: "demo"})
	if err != nil {
		t.Fatalf("getSiteTool: %v", err)
	}
	if out.Site.Name != "demo" {
		t.Errorf("Site.Name = %q", out.Site.Name)
	}
	if len(out.Domains) != 2 {
		t.Errorf("Domains = %v, want 2 entries", out.Domains)
	}
	if out.Metadata == nil {
		t.Error("expected non-nil Metadata map")
	}
}

func TestValidateSiteTool(t *testing.T) {
	withRoot(t)

	// Missing name → request error.
	if _, _, err := validateSiteTool(context.Background(), nil, validateSiteIn{}); err != nil {
		// name "" is reported as a request error
		t.Logf("empty-name error (expected): %v", err)
	} else {
		t.Error("expected error for empty name")
	}

	// No metadata on disk → OK=false, error in payload, nil call error.
	_, out, err := validateSiteTool(context.Background(), nil, validateSiteIn{Name: "ghost"})
	if err != nil {
		t.Fatalf("validateSiteTool (missing): unexpected call error %v", err)
	}
	if out.OK {
		t.Error("expected OK=false for missing metadata")
	}

	// Invalid metadata (no domains) → OK=false.
	if err := site.WriteSiteMetadata("bad", site.SiteMetadata{Type: site.SiteTypeStatic, ProjectPath: t.TempDir()}); err != nil {
		t.Fatalf("WriteSiteMetadata: %v", err)
	}
	_, out, err = validateSiteTool(context.Background(), nil, validateSiteIn{Name: "bad"})
	if err != nil {
		t.Fatalf("validateSiteTool (invalid): %v", err)
	}
	if out.OK || out.Error == "" {
		t.Errorf("expected OK=false with error, got %+v", out)
	}

	// Valid metadata → OK=true.
	if err := site.WriteSiteMetadata("good", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"good.test"},
		ProjectPath: t.TempDir(),
	}); err != nil {
		t.Fatalf("WriteSiteMetadata: %v", err)
	}
	_, out, err = validateSiteTool(context.Background(), nil, validateSiteIn{Name: "good"})
	if err != nil {
		t.Fatalf("validateSiteTool (valid): %v", err)
	}
	if !out.OK {
		t.Errorf("expected OK=true, got %+v", out)
	}
}

func TestProxyTools(t *testing.T) {
	withRoot(t)

	// Empty: no proxies registered.
	_, lout, err := listProxiesTool(context.Background(), nil, listProxiesIn{})
	if err != nil {
		t.Fatalf("listProxiesTool (empty): %v", err)
	}
	if len(lout.Proxies) != 0 {
		t.Fatalf("expected 0 proxies, got %v", lout.Proxies)
	}

	// get on a missing proxy → error.
	if _, _, err := getProxyTool(context.Background(), nil, getProxyIn{Name: "nope"}); err == nil {
		t.Error("expected error for unknown proxy")
	}
	// get with empty name → error.
	if _, _, err := getProxyTool(context.Background(), nil, getProxyIn{}); err == nil {
		t.Error("expected error for empty proxy name")
	}

	if err := proxy.Write(proxy.Metadata{
		Name:    "api",
		Domains: []string{"api.test"},
		IsLocal: true,
	}); err != nil {
		t.Fatalf("proxy.Write: %v", err)
	}

	_, lout, err = listProxiesTool(context.Background(), nil, listProxiesIn{})
	if err != nil {
		t.Fatalf("listProxiesTool: %v", err)
	}
	if len(lout.Proxies) != 1 || lout.Proxies[0] != "api" {
		t.Fatalf("expected [api], got %v", lout.Proxies)
	}

	_, gout, err := getProxyTool(context.Background(), nil, getProxyIn{Name: "api"})
	if err != nil {
		t.Fatalf("getProxyTool: %v", err)
	}
	// proxy.Metadata carries no json tags, so toJSONMap keys are the Go
	// field names ("Name"), not the yaml ones.
	if gout.Proxy["Name"] != "api" {
		t.Errorf("proxy map mismatch: %+v", gout.Proxy)
	}
}

func TestListRedirectsTool(t *testing.T) {
	root := withRoot(t)

	// Conf dir absent → empty list, no error.
	_, out, err := listRedirectsTool(context.Background(), nil, listRedirectsIn{})
	if err != nil {
		t.Fatalf("listRedirectsTool (empty): %v", err)
	}
	if len(out.Redirects) != 0 {
		t.Fatalf("expected 0 redirects, got %v", out.Redirects)
	}

	// Drop a redirect-<name>.yml into the conf dir and expect the short name.
	confDir := filepath.Join(root, "traefik", "conf")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatalf("mkdir conf: %v", err)
	}
	fname := constants.RedirectConfigPrefix + "old-site" + constants.ExtYAML
	if err := os.WriteFile(filepath.Join(confDir, fname), []byte("http: {}\n"), 0o644); err != nil {
		t.Fatalf("write redirect file: %v", err)
	}
	// A non-redirect file in the same dir must be ignored.
	if err := os.WriteFile(filepath.Join(confDir, "traefik.yml"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write decoy: %v", err)
	}

	_, out, err = listRedirectsTool(context.Background(), nil, listRedirectsIn{})
	if err != nil {
		t.Fatalf("listRedirectsTool: %v", err)
	}
	if len(out.Redirects) != 1 || out.Redirects[0] != "old-site" {
		t.Fatalf("expected [old-site], got %v", out.Redirects)
	}
}

func TestReloadSiteToolRequiresName(t *testing.T) {
	withRoot(t)
	if _, _, err := reloadSiteTool(context.Background(), nil, reloadSiteIn{}); err == nil {
		t.Error("expected error for empty name")
	}
}
