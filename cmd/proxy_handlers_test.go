package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/mkcert"
)

func TestValidateProxyInputMissingFlags(t *testing.T) {
	resetProxyAddFlags()
	if _, err := validateProxyInput(); err == nil {
		t.Error("expected err: missing --port and --container")
	}
}

func TestValidateProxyInputBothFlags(t *testing.T) {
	resetProxyAddFlags()
	proxyAddFlags.port = "8080"
	proxyAddFlags.container = "redis:6379"
	if _, err := validateProxyInput(); err == nil {
		t.Error("expected err: mutually exclusive")
	}
}

func TestValidateProxyInputBadDomain(t *testing.T) {
	resetProxyAddFlags()
	proxyAddFlags.domain = "bad domain"
	proxyAddFlags.port = "8080"
	if _, err := validateProxyInput(); err == nil {
		t.Error("expected err: invalid domain")
	}
}

func TestValidateProxyInputBadPort(t *testing.T) {
	resetProxyAddFlags()
	proxyAddFlags.domain = "x.local"
	proxyAddFlags.port = "notnum"
	if _, err := validateProxyInput(); err == nil {
		t.Error("expected err: invalid port")
	}
}

func TestValidateProxyInputBadContainerFormat(t *testing.T) {
	resetProxyAddFlags()
	proxyAddFlags.domain = "x.local"
	proxyAddFlags.container = "no-colon-format"
	if _, err := validateProxyInput(); err == nil {
		t.Error("expected err: bad container format")
	}
}

func TestValidateProxyInputLocalhost(t *testing.T) {
	resetProxyAddFlags()
	proxyAddFlags.domain = "blog.local"
	proxyAddFlags.port = "8080"
	in, err := validateProxyInput()
	if err != nil {
		t.Fatal(err)
	}
	if in.isContainer {
		t.Error("isContainer should be false")
	}
	if in.port != "8080" || in.domain != "blog.local" {
		t.Errorf("got %+v", in)
	}
}

func TestValidateProxyInputContainerMissing(t *testing.T) {
	resetProxyAddFlags()
	proxyAddFlags.domain = "x.local"
	proxyAddFlags.container = "ghost:6379"
	t.Cleanup(docker.SwapNewClientOK())
	if _, err := validateProxyInput(); err == nil {
		t.Error("expected err: container missing")
	}
}

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
	if err := writeProxyConfig(cfg, "blog", "blog.local", "http://host.docker.internal:8080", "", false); err != nil {
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

func TestSetupProxyCertificate(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(mkcert.SwapRunner(stubMkcertRunner{}))
	input := &proxyInput{name: "blog", domain: "blog.local", wildcard: false}
	if err := setupProxyCertificate(input); err != nil {
		t.Errorf("err: %v", err)
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

func TestConnectProxyContainerLocalhost(t *testing.T) {
	setupSrvRoot(t)
	cfg, _ := config.Load()
	input := &proxyInput{port: "65432"} // nothing listening on this random port
	got, err := connectProxyContainer(input, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got == "" {
		t.Error("expected URL")
	}
}

func TestConnectProxyContainerContainer(t *testing.T) {
	setupSrvRoot(t)
	cfg, _ := config.Load()
	t.Cleanup(docker.SwapNewClientOK())
	input := &proxyInput{
		isContainer:   true,
		containerName: "redis",
		containerPort: "6379",
	}
	got, err := connectProxyContainer(input, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://redis:6379" {
		t.Errorf("got %q", got)
	}
}

func TestRunProxyAddLocalhost(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientOK())
	t.Cleanup(mkcert.SwapRunner(stubMkcertRunner{}))
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
	if err := writeProxyConfig(cfg, "blog", "blog.local", "http://x:8080", "", false); err != nil {
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

func TestSetupRedirectCertificate(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(mkcert.SwapRunner(stubMkcertRunner{}))
	input := &redirectInput{name: "alias", domain: "old.local"}
	if err := setupRedirectCertificate(input); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunProxyAddContainer(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientOK())
	t.Cleanup(mkcert.SwapRunner(stubMkcertRunner{}))
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
	t.Cleanup(mkcert.SwapRunner(stubMkcertRunner{}))
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
	t.Cleanup(mkcert.SwapRunner(stubMkcertRunner{}))
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
	if err := writeProxyConfig(cfg, "blog", "blog.local", "http://x:8080", "", false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(docker.SwapNewClientOK())
	t.Cleanup(mkcert.SwapRunner(stubMkcertRunner{}))
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
	t.Cleanup(mkcert.SwapRunner(stubMkcertRunner{}))
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
	if err := writeProxyConfig(cfg, "blog", "blog.local", "http://host.docker.internal:8080", "", false); err != nil {
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
