package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/config"
)

func newCmdCfg(t *testing.T) *config.Config {
	t.Helper()
	root := t.TempDir()
	cfg := &config.Config{
		Root:        root,
		TraefikDir:  filepath.Join(root, "traefik"),
		SitesDir:    filepath.Join(root, "sites"),
		NetworkName: "n",
	}
	if err := os.MkdirAll(cfg.TraefikConfDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestWriteProxyConfigLocalhost(t *testing.T) {
	cfg := newCmdCfg(t)
	if err := writeProxyConfig(cfg, "blog", "blog.local", "http://host.docker.internal:8080", "", false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.TraefikConfDir(), "proxy-blog.yml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "blog.local") {
		t.Error("domain missing")
	}
	if !strings.Contains(body, "host.docker.internal:8080") {
		t.Error("target missing")
	}
	if strings.Contains(body, "# Container:") {
		t.Error("localhost config should not have Container header")
	}
}

func TestWriteProxyConfigContainer(t *testing.T) {
	cfg := newCmdCfg(t)
	if err := writeProxyConfig(cfg, "redis", "redis.local", "http://redis:6379", "redis", false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(cfg.TraefikConfDir(), "proxy-redis.yml"))
	if !strings.Contains(string(data), "# Container: redis") {
		t.Errorf("container header missing: %q", string(data))
	}
}

func TestReadProxyConfigMissing(t *testing.T) {
	cfg := newCmdCfg(t)
	info := readProxyConfig(cfg, "ghost")
	if info.Target != "unknown" {
		t.Errorf("missing should yield Target=unknown, got %+v", info)
	}
}

func TestReadProxyConfigRoundtrip(t *testing.T) {
	cfg := newCmdCfg(t)
	if err := writeProxyConfig(cfg, "blog", "blog.local", "http://host.docker.internal:8080", "", false); err != nil {
		t.Fatal(err)
	}
	info := readProxyConfig(cfg, "blog")
	if info.Domain != "blog.local" {
		t.Errorf("Domain = %q", info.Domain)
	}
	if info.Target == "" || info.Target == "unknown" {
		t.Errorf("Target should be populated, got %q", info.Target)
	}
}

func TestReadProxyConfigBadYAML(t *testing.T) {
	cfg := newCmdCfg(t)
	path := filepath.Join(cfg.TraefikConfDir(), "proxy-bad.yml")
	if err := os.WriteFile(path, []byte(":::not yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	info := readProxyConfig(cfg, "bad")
	if info.Target != "unknown" {
		t.Errorf("bad yaml should yield unknown, got %+v", info)
	}
}
