package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/mkcert"
	"github.com/stubbedev/srv/internal/traefik"
)

func TestRunProxyListEmpty(t *testing.T) {
	setupSrvRoot(t)
	if err := runProxyList(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunProxyRemoveMissing(t *testing.T) {
	setupSrvRoot(t)
	if err := runProxyRemove(nil, []string{"ghost"}); err == nil {
		t.Error("expected err")
	}
}

func TestRunProxyRemoveExisting(t *testing.T) {
	setupSrvRoot(t)
	cfg, _ := config.Load()
	if err := traefik.WriteProxyConfig(cfg, traefik.ProxyRoute{Name: "blog", Domain: "blog.local", TargetURL: "http://host.docker.internal:8080"}); err != nil {
		t.Fatal(err)
	}
	if err := runProxyRemove(nil, []string{"blog"}); err != nil {
		t.Errorf("err: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.TraefikConfDir(), "proxy-blog.yml")); !os.IsNotExist(err) {
		t.Errorf("proxy file should be gone: %v", err)
	}
}

func TestGetProxySSLStatusEmptyDomain(t *testing.T) {
	if out := getProxySSLStatus("x", ""); out == "" {
		t.Error("expected dim placeholder")
	}
}

func TestGetProxyNamesEmpty(t *testing.T) {
	setupSrvRoot(t)
	if got := getProxyNames(); len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestGetProxyNamesFinds(t *testing.T) {
	setupSrvRoot(t)
	cfg, _ := config.Load()
	if err := os.WriteFile(filepath.Join(cfg.TraefikConfDir(), "proxy-a.yml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.TraefikConfDir(), "other.yml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	names := getProxyNames()
	if len(names) != 1 || names[0] != "a" {
		t.Errorf("got %v", names)
	}
}

func resetProxyAddFlags() {
	proxyAddFlags.domain = ""
	proxyAddFlags.port = ""
	proxyAddFlags.container = ""
	proxyAddFlags.name = ""
	proxyAddFlags.wildcard = false
	proxyAddFlags.force = false
	proxyAddFlags.fallbackURL = ""
	proxyAddFlags.fallbackTimeout = ""
}

func TestRunProxyAddLocalhost(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientOK())
	t.Cleanup(mkcert.SwapEngine(&stubMkcertEngine{}))
	resetProxyAddFlags()
	proxyAddFlags.domain = "blog.local"
	proxyAddFlags.port = "8080"
	proxyAddFlags.name = "blog"
	defer resetProxyAddFlags()
	if err := runProxyAdd(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunProxyAddBadInput(t *testing.T) {
	setupSrvRoot(t)
	resetProxyAddFlags()
	defer resetProxyAddFlags()
	if err := runProxyAdd(nil, nil); err == nil {
		t.Error("expected err: missing flags")
	}
}

func TestRunProxyAddExisting(t *testing.T) {
	setupSrvRoot(t)
	cfg, _ := config.Load()
	if err := traefik.WriteProxyConfig(cfg, traefik.ProxyRoute{Name: "blog", Domain: "blog.local", TargetURL: "http://x:8080"}); err != nil {
		t.Fatal(err)
	}
	resetProxyAddFlags()
	proxyAddFlags.domain = "blog.local"
	proxyAddFlags.port = "8080"
	proxyAddFlags.name = "blog"
	defer resetProxyAddFlags()
	if err := runProxyAdd(nil, nil); err == nil {
		t.Error("expected err: exists, no --force")
	}
}

func TestRunProxyAddContainer(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientOK())
	t.Cleanup(mkcert.SwapEngine(&stubMkcertEngine{}))
	resetProxyAddFlags()
	proxyAddFlags.domain = "redis.local"
	proxyAddFlags.container = "redis:6379"
	proxyAddFlags.name = "redis"
	defer resetProxyAddFlags()
	// noopSDK.ContainerInspect returns err; ContainerExists therefore returns
	// false, so validateProxyInput should error out.
	if err := runProxyAdd(nil, nil); err == nil {
		t.Error("expected err: container missing")
	}
}

func TestRunProxyAddWildcard(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientOK())
	t.Cleanup(mkcert.SwapEngine(&stubMkcertEngine{}))
	resetProxyAddFlags()
	proxyAddFlags.domain = "blog.local"
	proxyAddFlags.port = "8080"
	proxyAddFlags.name = "blog"
	proxyAddFlags.wildcard = true
	defer resetProxyAddFlags()
	if err := runProxyAdd(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunProxyAddFallback(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientOK())
	t.Cleanup(mkcert.SwapEngine(&stubMkcertEngine{}))
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	resetProxyAddFlags()
	proxyAddFlags.domain = "blog.local"
	proxyAddFlags.port = "8080"
	proxyAddFlags.name = "blog"
	proxyAddFlags.fallbackURL = "https://backup.example.com"
	proxyAddFlags.fallbackTimeout = "3s"
	defer resetProxyAddFlags()
	if err := runProxyAdd(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunProxyAddForceOverwrite(t *testing.T) {
	setupSrvRoot(t)
	cfg, _ := config.Load()
	if err := traefik.WriteProxyConfig(cfg, traefik.ProxyRoute{Name: "blog", Domain: "blog.local", TargetURL: "http://x:8080"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(docker.SwapNewClientOK())
	t.Cleanup(mkcert.SwapEngine(&stubMkcertEngine{}))
	resetProxyAddFlags()
	proxyAddFlags.domain = "blog.local"
	proxyAddFlags.port = "8080"
	proxyAddFlags.name = "blog"
	proxyAddFlags.force = true
	defer resetProxyAddFlags()
	if err := runProxyAdd(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunRedirectAddHTTP(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(mkcert.SwapEngine(&stubMkcertEngine{}))
	resetRedirectFlags()
	redirectAddFlags.domain = "old.com"
	redirectAddFlags.to = "https://new.com"
	redirectAddFlags.name = "alias"
	defer resetRedirectFlags()
	if err := runRedirectAdd(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunProxyListWithProxies(t *testing.T) {
	setupSrvRoot(t)
	cfg, _ := config.Load()
	if err := traefik.WriteProxyConfig(cfg, traefik.ProxyRoute{Name: "blog", Domain: "blog.local", TargetURL: "http://host.docker.internal:8080"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(docker.SwapNewClientErr(errors.New("offline")))
	if err := runProxyList(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunRedirectAddExisting(t *testing.T) {
	setupSrvRoot(t)
	cfg, _ := config.Load()
	path := cfg.TraefikConfDir() + "/redirect-alias.yml"
	if err := writeFile2(path, "x"); err != nil {
		t.Fatal(err)
	}
	resetRedirectFlags()
	redirectAddFlags.domain = "old.com"
	redirectAddFlags.to = "https://new.com"
	redirectAddFlags.name = "alias"
	redirectAddFlags.force = false
	defer resetRedirectFlags()
	if err := runRedirectAdd(nil, nil); err == nil {
		t.Error("expected err: exists without --force")
	}
}
