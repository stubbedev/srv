package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/config"
)

func newSiteCfg(t *testing.T) *config.Config {
	t.Helper()
	root := t.TempDir()
	return &config.Config{
		Root:        root,
		TraefikDir:  filepath.Join(root, "traefik"),
		SitesDir:    filepath.Join(root, "sites"),
		NetworkName: "tnet",
	}
}

func TestReloadStatePath(t *testing.T) {
	cfg := newSiteCfg(t)
	got := reloadStatePath(cfg, "blog")
	want := filepath.Join(cfg.SitesDir, "blog", ".reload-state")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestComputeMetadataHashStable(t *testing.T) {
	meta := &SiteMetadata{Type: SiteTypeStatic, Domains: []string{"x.local"}, ProjectPath: "/srv/x", Port: 80}
	a := computeMetadataHash(meta)
	b := computeMetadataHash(meta)
	if a == "" {
		t.Fatal("empty hash")
	}
	if a != b {
		t.Error("hash not stable")
	}
}

func TestComputeMetadataHashDiffersOnChange(t *testing.T) {
	a := computeMetadataHash(&SiteMetadata{Type: SiteTypeStatic, Domains: []string{"a.local"}})
	b := computeMetadataHash(&SiteMetadata{Type: SiteTypeStatic, Domains: []string{"b.local"}})
	if a == b {
		t.Error("expected different hashes for different domains")
	}
}

func TestReadWriteLastReloadHash(t *testing.T) {
	cfg := newSiteCfg(t)
	siteDir := SiteConfigDir(cfg, "blog")
	if err := os.MkdirAll(siteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLastReloadHash(cfg, "blog", "abc123")
	if got := readLastReloadHash(cfg, "blog"); got != "abc123" {
		t.Errorf("got %q, want abc123", got)
	}
}

func TestReadLastReloadHashMissing(t *testing.T) {
	cfg := newSiteCfg(t)
	if got := readLastReloadHash(cfg, "missing"); got != "" {
		t.Errorf("missing -> %q, want empty", got)
	}
}

func TestSiteComposePath(t *testing.T) {
	cfg := newSiteCfg(t)
	got := SiteComposePath(cfg, "blog")
	want := filepath.Join(cfg.SitesDir, "blog", "docker-compose.yml")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSiteNginxConfPath(t *testing.T) {
	cfg := newSiteCfg(t)
	got := SiteNginxConfPath(cfg, "blog")
	if filepath.Base(got) != "nginx.conf" {
		t.Errorf("expected nginx.conf, got %q", filepath.Base(got))
	}
}

func TestPrimaryDomain(t *testing.T) {
	m := &SiteMetadata{Domains: []string{"a.local", "b.local"}}
	if m.PrimaryDomain() != "a.local" {
		t.Errorf("PrimaryDomain = %q", m.PrimaryDomain())
	}
	empty := &SiteMetadata{}
	if empty.PrimaryDomain() != "" {
		t.Error("empty domains should return empty")
	}
	var nilM *SiteMetadata
	if nilM.PrimaryDomain() != "" {
		t.Error("nil should return empty")
	}
}

