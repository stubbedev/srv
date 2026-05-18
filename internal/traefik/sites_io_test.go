package traefik

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
