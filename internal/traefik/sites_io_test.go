package traefik

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestWriteSiteRouteConfigLocal(t *testing.T) {
	cfg := newTraefikCfg(t)
	route := SiteRouteConfig{
		Name:        "blog",
		Domains:     []string{"blog.local"},
		ServiceName: "srv-blog-web",
		Port:        80,
		IsLocal:     true,
	}
	if err := WriteSiteRouteConfig(cfg, route); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.TraefikConfDir(), "site-blog.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "blog.local") {
		t.Error("domain missing")
	}
	if !strings.Contains(body, "http://srv-blog-web:80") {
		t.Error("service URL missing")
	}
	if strings.Contains(body, "certResolver") {
		t.Error("local site should not reference certResolver")
	}
}

func TestWriteSiteRouteConfigProduction(t *testing.T) {
	cfg := newTraefikCfg(t)
	route := SiteRouteConfig{
		Name:        "blog",
		Domains:     []string{"blog.com"},
		ServiceName: "srv-blog-web",
		Port:        80,
		IsLocal:     false,
	}
	if err := WriteSiteRouteConfig(cfg, route); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(cfg.TraefikConfDir(), "site-blog.yml"))
	if !strings.Contains(string(data), "letsencrypt") {
		t.Error("certResolver missing for non-local")
	}
}

func TestWriteSiteRouteConfigInternalListener(t *testing.T) {
	cfg := newTraefikCfg(t)
	route := SiteRouteConfig{
		Name:        "blog",
		Domains:     []string{"blog.local"},
		ServiceName: "srv-blog-web",
		Port:        80,
		IsLocal:     true,
		Listeners:   []string{"internal"},
	}
	if err := WriteSiteRouteConfig(cfg, route); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(cfg.TraefikConfDir(), "site-blog.yml"))
	body := string(data)
	if !strings.Contains(body, "site-blog-internal") {
		t.Error("internal router missing")
	}
}

func TestRemoveSiteRouteConfigMissing(t *testing.T) {
	cfg := newTraefikCfg(t)
	if err := RemoveSiteRouteConfig(cfg, "ghost"); err != nil {
		t.Errorf("removing missing site config: %v", err)
	}
}

func TestRemoveSiteRouteConfigExisting(t *testing.T) {
	cfg := newTraefikCfg(t)
	path := filepath.Join(cfg.TraefikConfDir(), "site-x.yml")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveSiteRouteConfig(cfg, "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file should be removed")
	}
}

func TestReadSiteRouteDomain(t *testing.T) {
	cfg := newTraefikCfg(t)
	if got := ReadSiteRouteDomain(cfg, "missing"); got != "" {
		t.Errorf("missing -> %q, want empty", got)
	}

	route := SiteRouteConfig{
		Name:        "blog",
		Domains:     []string{"blog.local"},
		ServiceName: "srv-blog-web",
		Port:        80,
		IsLocal:     true,
	}
	if err := WriteSiteRouteConfig(cfg, route); err != nil {
		t.Fatal(err)
	}
	if got := ReadSiteRouteDomain(cfg, "blog"); got != "blog.local" {
		t.Errorf("got %q, want blog.local", got)
	}
}

func TestReadSiteRouteDomainBadYAML(t *testing.T) {
	cfg := newTraefikCfg(t)
	path := filepath.Join(cfg.TraefikConfDir(), "site-bad.yml")
	if err := os.WriteFile(path, []byte(": :"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ReadSiteRouteDomain(cfg, "bad"); got != "" {
		t.Errorf("bad YAML -> %q, want empty", got)
	}
}

// Daemon-served sites get a compress middleware on every router (the nginx
// renderer gzips; the daemon's server does not), container sites do not.
func TestWriteSiteRouteConfigDaemonServedCompresses(t *testing.T) {
	cfg := newTraefikCfg(t)
	read := func(route SiteRouteConfig) DynConfig {
		t.Helper()
		if err := WriteSiteRouteConfig(cfg, route); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(SiteRouteConfigPath(cfg, route.Name))
		if err != nil {
			t.Fatal(err)
		}
		var dc DynConfig
		if err := yaml.Unmarshal(data, &dc); err != nil {
			t.Fatalf("%v\n%s", err, data)
		}
		return dc
	}

	dc := read(SiteRouteConfig{
		Name: "docs", Domains: []string{"docs.test"}, IsLocal: true,
		Listeners: []string{"internal"}, DaemonServed: true,
	})
	mw, ok := dc.HTTP.Middlewares["site-docs-compress"]
	if !ok || mw.Compress == nil {
		t.Fatalf("compress middleware missing: %+v", dc.HTTP.Middlewares)
	}
	if len(dc.HTTP.Routers) != 2 {
		t.Fatalf("routers = %d, want websecure + internal", len(dc.HTTP.Routers))
	}
	for name, r := range dc.HTTP.Routers {
		if len(r.Middlewares) != 1 || r.Middlewares[0] != "site-docs-compress" {
			t.Errorf("router %s middlewares = %v, want the compress middleware", name, r.Middlewares)
		}
	}

	dc = read(SiteRouteConfig{Name: "app", Domains: []string{"app.test"}, ServiceName: "app", Port: 80, IsLocal: true})
	if len(dc.HTTP.Middlewares) != 0 {
		t.Errorf("container site got middlewares %v; its app owns its encoding", dc.HTTP.Middlewares)
	}
}
