package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
)

func TestFallbackSiteDir(t *testing.T) {
	cfg := &config.Config{SitesDir: "/srv/sites"}
	got := fallbackSiteDir(cfg, "blog")
	if got != "/srv/sites/_proxy-blog-fallback" {
		t.Errorf("got %q", got)
	}
}

func TestRenderFallbackComposeHostNetwork(t *testing.T) {
	spec := fallbackSpec{
		Name:        "blog",
		HostNetwork: true,
		PrimaryHost: "127.0.0.1",
		PrimaryPort: "8080",
	}
	out := renderFallbackCompose(spec, "/etc/nginx", "tnet")
	if !strings.Contains(out, "network_mode: host") {
		t.Error("missing host networking")
	}
	if !strings.Contains(out, "/etc/nginx/nginx.conf") {
		t.Error("missing nginx conf bind mount")
	}
}

func TestRenderFallbackComposeBridge(t *testing.T) {
	spec := fallbackSpec{Name: "blog", PrimaryHost: "redis", PrimaryPort: "6379"}
	out := renderFallbackCompose(spec, "/etc/nginx", "tnet")
	if !strings.Contains(out, "tnet") {
		t.Error("missing network name")
	}
	if strings.Contains(out, "network_mode: host") {
		t.Error("bridge should not use host networking")
	}
}

func TestRemoveFallbackSidecarMissing(t *testing.T) {
	cfg := newCmdCfg(t)
	if err := removeFallbackSidecar(cfg, "ghost"); err != nil {
		t.Errorf("missing sidecar should be no-op: %v", err)
	}
}

func TestRemoveFallbackSidecarExisting(t *testing.T) {
	cfg := newCmdCfg(t)
	dir := fallbackSiteDir(cfg, "blog")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	if err := removeFallbackSidecar(cfg, "blog"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("dir should be gone: %v", err)
	}
}

func TestWriteFallbackSidecarHostNetwork(t *testing.T) {
	cfg := newCmdCfg(t)
	if err := os.MkdirAll(cfg.SitesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	spec := fallbackSpec{
		Name:        "blog",
		FallbackURL: "https://prod.example.com",
		HostNetwork: true,
		PrimaryHost: "127.0.0.1",
		PrimaryPort: "8080",
	}
	got, err := writeFallbackSidecar(cfg, spec)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "http://127.0.0.1:") {
		t.Errorf("expected loopback URL, got %q", got)
	}
}

func TestWriteFallbackSidecarBridge(t *testing.T) {
	cfg := newCmdCfg(t)
	if err := os.MkdirAll(cfg.SitesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	spec := fallbackSpec{
		Name:        "blog",
		FallbackURL: "https://prod.example.com",
		PrimaryHost: "redis",
		PrimaryPort: "6379",
	}
	got, err := writeFallbackSidecar(cfg, spec)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, fallbackContainerName("blog")) {
		t.Errorf("expected container name in URL: %q", got)
	}
}

func TestRenderFallbackNginxHostNetworkLoopback(t *testing.T) {
	out, err := renderFallbackNginx(fallbackSpec{
		Name:        "blog",
		FallbackURL: "https://prod.example.com",
		HostNetwork: true,
		ListenPort:  5555,
		PrimaryHost: "127.0.0.1",
		PrimaryPort: "8080",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "listen 127.0.0.1:5555;") {
		t.Errorf("expected loopback listen: %q", out[:200])
	}
}

func TestRenderFallbackNginxDefaultTimeout(t *testing.T) {
	out, err := renderFallbackNginx(fallbackSpec{
		Name:        "blog",
		FallbackURL: "https://prod.example.com",
		PrimaryHost: "redis",
		PrimaryPort: "6379",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "proxy_connect_timeout 2s;") {
		t.Error("default timeout should be 2s")
	}
}

func TestRenderFallbackNginx(t *testing.T) {
	conf, err := renderFallbackNginx(fallbackSpec{
		Name:            "kontainer",
		PrimaryHost:     "host.docker.internal",
		PrimaryPort:     "3001",
		FallbackURL:     "https://kontainer.com",
		FallbackTimeout: "3s",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	mustContain := []string{
		"proxy_pass http://host.docker.internal:3001;",
		"error_page 502 503 504 = @fallback;",
		"location @fallback {",
		"proxy_pass https://$fb_host:443$request_uri;",
		`set $fb_host "kontainer.com";`,
		"proxy_connect_timeout 3s;",
		"proxy_ssl_server_name on;",
	}
	for _, want := range mustContain {
		if !strings.Contains(conf, want) {
			t.Errorf("missing %q in:\n%s", want, conf)
		}
	}
}

func TestRenderFallbackNginx_BadURL(t *testing.T) {
	tests := []string{
		"ftp://x",
		"notaurl",
	}
	for _, u := range tests {
		_, err := renderFallbackNginx(fallbackSpec{
			Name:        "x",
			PrimaryHost: "host.docker.internal",
			PrimaryPort: "80",
			FallbackURL: u,
		})
		if err == nil {
			t.Errorf("expected error for %q", u)
		}
	}
}
