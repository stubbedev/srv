package cmd

import (
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/mkcert"
)

func mustLoadConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestGenerateLocalCertEmpty(t *testing.T) {
	// Empty domains is a no-op; just ensure no panic.
	generateLocalCert("blog", nil, false)
}

func TestGenerateLocalCertHappy(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(mkcert.SwapRunner(stubMkcertRunner{}))
	generateLocalCert("blog", []string{"blog.local"}, false)
}

func TestRenewLocalCertIfNeededEmpty(t *testing.T) {
	renewLocalCertIfNeeded("blog", nil, false)
}

func TestRenewLocalCertIfNeededNewCert(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(mkcert.SwapRunner(stubMkcertRunner{}))
	// No cert exists → cert is regenerated.
	renewLocalCertIfNeeded("blog", []string{"blog.local"}, false)
}

func TestGetRedirectSSLStatusEmpty(t *testing.T) {
	if got := getRedirectSSLStatus("name", ""); got == "" {
		t.Error("expected dim placeholder")
	}
}

func TestGetRedirectSSLStatusMissing(t *testing.T) {
	setupSrvRoot(t)
	if got := getRedirectSSLStatus("name", "x.local"); got == "" {
		t.Error("expected status")
	}
}

func TestStartSiteAfterAddStatic(t *testing.T) {
	setupSrvRoot(t)
	cfg := mustLoadConfig(t)
	setup := &siteSetup{
		siteName: "blog",
		sitePath: t.TempDir(),
		isStatic: true,
	}
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	if err := startSiteAfterAdd(cfg, setup); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestStartSiteAfterAddCompose(t *testing.T) {
	setupSrvRoot(t)
	cfg := mustLoadConfig(t)
	setup := &siteSetup{
		siteName: "x",
		sitePath: t.TempDir(),
		profile:  "dev",
	}
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	if err := startSiteAfterAdd(cfg, setup); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestFinalizeSiteSetupStatic(t *testing.T) {
	setupSrvRoot(t)
	cfg := mustLoadConfig(t)
	setup := &siteSetup{
		siteName: "blog",
		sitePath: t.TempDir(),
		domain:   "blog.local",
		isStatic: true,
	}
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	if err := finalizeSiteSetup(cfg, setup); err != nil {
		t.Errorf("err: %v", err)
	}
}

