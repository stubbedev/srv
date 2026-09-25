package proxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
)

func TestFallbackDir(t *testing.T) {
	cfg := &config.Config{SitesDir: "/srv/sites"}
	if got := FallbackDir(cfg, "blog"); got != "/srv/sites/_proxy-blog-fallback" {
		t.Errorf("got %q", got)
	}
}

func TestFallbackContainerName(t *testing.T) {
	if got := FallbackContainerName("blog"); got != "srv-proxy-blog-fallback" {
		t.Errorf("got %q", got)
	}
}

func TestFindFreeLoopbackPort(t *testing.T) {
	port, err := findFreeLoopbackPort()
	if err != nil {
		t.Fatal(err)
	}
	if port <= 0 || port > 65535 {
		t.Errorf("port out of range: %d", port)
	}
}

func TestRenderFallbackComposeHostNetwork(t *testing.T) {
	spec := FallbackSpec{
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
	spec := FallbackSpec{Name: "blog", PrimaryHost: "redis", PrimaryPort: "6379"}
	out := renderFallbackCompose(spec, "/etc/nginx", "tnet")
	if !strings.Contains(out, "tnet") {
		t.Error("missing network name")
	}
	if strings.Contains(out, "network_mode: host") {
		t.Error("bridge should not use host networking")
	}
}

func TestRemoveFallbackSidecarMissing(t *testing.T) {
	cfg := addEnv(t)
	if err := RemoveFallbackSidecar(cfg, "ghost"); err != nil {
		t.Errorf("missing sidecar should be no-op: %v", err)
	}
}

func TestRemoveFallbackSidecarExisting(t *testing.T) {
	cfg := addEnv(t)
	dir := FallbackDir(cfg, "blog")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveFallbackSidecar(cfg, "blog"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("dir should be gone: %v", err)
	}
}

func TestEnsureFallbackSidecarHostNetwork(t *testing.T) {
	cfg := addEnv(t)
	if err := os.MkdirAll(cfg.SitesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := EnsureFallbackSidecar(cfg, FallbackSpec{
		Name:        "blog",
		FallbackURL: "https://prod.example.com",
		HostNetwork: true,
		PrimaryHost: "127.0.0.1",
		PrimaryPort: "8080",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "http://127.0.0.1:") {
		t.Errorf("expected loopback URL, got %q", got)
	}
}

func TestEnsureFallbackSidecarBridge(t *testing.T) {
	cfg := addEnv(t)
	if err := os.MkdirAll(cfg.SitesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := EnsureFallbackSidecar(cfg, FallbackSpec{
		Name:        "blog",
		FallbackURL: "https://prod.example.com",
		PrimaryHost: "redis",
		PrimaryPort: "6379",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, FallbackContainerName("blog")) {
		t.Errorf("expected container name in URL: %q", got)
	}
}

func TestRenderFallbackNginxHostNetworkLoopback(t *testing.T) {
	out, err := renderFallbackNginx(FallbackSpec{
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
	out, err := renderFallbackNginx(FallbackSpec{
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
	conf, err := renderFallbackNginx(FallbackSpec{
		Name:            "myapp",
		PrimaryHost:     "host.docker.internal",
		PrimaryPort:     "3001",
		FallbackURL:     "https://myapp.com",
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
		`set $fb_host "myapp.com";`,
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
	for _, u := range []string{"ftp://x", "notaurl"} {
		if _, err := renderFallbackNginx(FallbackSpec{
			Name:        "x",
			PrimaryHost: "host.docker.internal",
			PrimaryPort: "80",
			FallbackURL: u,
		}); err == nil {
			t.Errorf("expected error for %q", u)
		}
	}
}

// TestAddFallbackSidecarLifecycle pins the rule that removed the orphaned
// sidecar: Add with a FallbackURL records it in metadata and starts the
// sidecar, and RemoveProxy tears the sidecar down — including for legacy
// proxies whose metadata never recorded the fallback.
func TestAddFallbackSidecarLifecycle(t *testing.T) {
	cfg := addEnv(t)
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))

	res, err := Add(cfg, AddSpec{Domain: "app.test", Port: "8080", FallbackURL: "https://prod.example.com", FallbackTimeout: "3s"})
	if err != nil {
		t.Fatalf("Add() = %v", err)
	}
	if !res.FallbackEnabled {
		t.Error("FallbackEnabled = false, want true")
	}
	if !strings.Contains(res.TargetURL, FallbackContainerName("app-test")) && !strings.HasPrefix(res.TargetURL, "http://127.0.0.1:") {
		t.Errorf("target should be the sidecar, got %q", res.TargetURL)
	}
	meta, err := Read("app-test")
	if err != nil || meta == nil {
		t.Fatalf("Read metadata: %v", err)
	}
	if meta.FallbackURL != "https://prod.example.com" || meta.FallbackTimeout != "3s" {
		t.Errorf("fallback not recorded in metadata: %+v", meta)
	}
	if _, err := os.Stat(FallbackDir(cfg, "app-test")); err != nil {
		t.Errorf("sidecar dir missing: %v", err)
	}

	if _, err := RemoveProxy(cfg, "app-test"); err != nil {
		t.Fatalf("RemoveProxy() = %v", err)
	}
	if _, err := os.Stat(FallbackDir(cfg, "app-test")); !os.IsNotExist(err) {
		t.Errorf("sidecar dir should be gone after remove: %v", err)
	}
}

// Legacy proxies (pre-metadata-fallback) are torn down by directory presence.
func TestRemoveProxyTearsDownLegacySidecarDir(t *testing.T) {
	cfg := addEnv(t)
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))

	if _, err := Add(cfg, AddSpec{Domain: "old.test", Port: "9000"}); err != nil {
		t.Fatalf("Add() = %v", err)
	}
	dir := FallbackDir(cfg, "old-test")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services: {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveProxy(cfg, "old-test"); err != nil {
		t.Fatalf("RemoveProxy() = %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("legacy sidecar dir should be gone: %v", err)
	}
}
