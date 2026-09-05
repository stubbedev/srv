package proxy

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/mkcert"
	"github.com/stubbedev/srv/internal/shell"
	"github.com/stubbedev/srv/internal/shell/shelltest"
	"github.com/stubbedev/srv/internal/site"
)

// mkcertStub stands in for the mkcert binary. Add() only needs cert generation
// to *succeed*; the bytes never matter here because nothing in this package
// parses them.
type mkcertStub struct{ outErr error }

func (m mkcertStub) Stream(...string) error { return nil }

// Output is the method cert generation goes through (mkcert.RunQuiet), so it
// is the one a failure test drives.
func (m mkcertStub) Output(...string) ([]byte, error) {
	return []byte("/root/mkcert\n"), m.outErr
}

func (m mkcertStub) Combined(...string) ([]byte, error) {
	return []byte("Created a new local CA"), nil
}

// addEnv gives a test an isolated SRV_ROOT plus stubs for every process srv
// would otherwise shell out to or dial. Returns the loaded config.
func addEnv(t *testing.T) *config.Config {
	t.Helper()
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	config.ResetCache()
	t.Cleanup(config.ResetCache)

	// SwapRunner alone is not enough: CheckMkcert asks exec.LookPath directly,
	// which fails in a sandboxed build (nix) where mkcert is not installed.
	t.Cleanup(mkcert.SwapLookPath(func(string) (string, error) { return "/usr/bin/mkcert", nil }))
	t.Cleanup(mkcert.SwapRunner(mkcertStub{}))
	t.Cleanup(shell.SwapDefault(shelltest.New(nil)))
	// Without these the reload paths reach a real `docker compose` and
	// recreate the developer's own srv_dns / srv_proxy containers mid-test.
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	t.Cleanup(docker.SwapComposePrefixedExec(func(string, string, ...string) error { return nil }))
	t.Cleanup(docker.SwapDockerExec(func(bool, ...string) error { return nil }))
	t.Cleanup(docker.SwapNewClientOK())

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.TraefikConfDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func proxyConfigPath(cfg *config.Config, name string) string {
	return filepath.Join(cfg.TraefikConfDir(), constants.ProxyConfigPrefix+name+constants.ExtYAML)
}

// ─── Add ─────────────────────────────────────────────────────────────────

func TestAddLocalhostPortWritesConfigAndMetadata(t *testing.T) {
	cfg := addEnv(t)

	res, err := Add(cfg, AddSpec{Domain: "app.test", Port: "8080"})
	if err != nil {
		t.Fatalf("Add() = %v", err)
	}
	if res.Name != "app-test" {
		t.Errorf("name = %q, want app-test (derived from the domain)", res.Name)
	}
	if res.Domain != "app.test" {
		t.Errorf("domain = %q", res.Domain)
	}
	if !strings.Contains(res.TargetURL, "8080") {
		t.Errorf("target URL = %q, want it to carry the port", res.TargetURL)
	}

	if _, err := os.Stat(proxyConfigPath(cfg, "app-test")); err != nil {
		t.Errorf("Traefik proxy config was not written: %v", err)
	}
	meta, err := Read("app-test")
	if err != nil || meta == nil {
		t.Fatalf("metadata sidecar missing: %v", err)
	}
	if len(meta.Domains) != 1 || meta.Domains[0] != "app.test" {
		t.Errorf("metadata domains = %v", meta.Domains)
	}
	if !meta.IsLocal {
		t.Error("proxy metadata should be marked local")
	}
}

func TestAddHonoursExplicitName(t *testing.T) {
	cfg := addEnv(t)
	res, err := Add(cfg, AddSpec{Name: "custom", Domain: "app.test", Port: "8080"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Name != "custom" {
		t.Errorf("name = %q, want custom", res.Name)
	}
	if _, err := os.Stat(proxyConfigPath(cfg, "custom")); err != nil {
		t.Errorf("config not written under the explicit name: %v", err)
	}
}

func TestAddWildcard(t *testing.T) {
	cfg := addEnv(t)
	if _, err := Add(cfg, AddSpec{Domain: "app.test", Port: "8080", Wildcard: true}); err != nil {
		t.Fatal(err)
	}
	meta, _ := Read("app-test")
	if meta == nil || !meta.Wildcard {
		t.Error("wildcard flag did not reach the metadata sidecar")
	}
}

// Re-adding without Force must refuse rather than silently clobber a proxy the
// user already has traffic pointed at.
func TestAddRefusesExistingProxyWithoutForce(t *testing.T) {
	cfg := addEnv(t)
	if _, err := Add(cfg, AddSpec{Domain: "app.test", Port: "8080"}); err != nil {
		t.Fatal(err)
	}
	_, err := Add(cfg, AddSpec{Domain: "app.test", Port: "9090"})
	if err == nil {
		t.Fatal("second Add() = nil, want a refusal")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v, want an 'already exists' refusal", err)
	}
}

func TestAddForceOverwrites(t *testing.T) {
	cfg := addEnv(t)
	if _, err := Add(cfg, AddSpec{Domain: "app.test", Port: "8080"}); err != nil {
		t.Fatal(err)
	}
	res, err := Add(cfg, AddSpec{Domain: "app.test", Port: "9090", Force: true})
	if err != nil {
		t.Fatalf("forced Add() = %v", err)
	}
	if !strings.Contains(res.TargetURL, "9090") {
		t.Errorf("target URL = %q, want the new port", res.TargetURL)
	}
}

// Force is an overwrite of the *route*, not a reset of the proxy: routes the
// user attached with `srv route add` have to survive it.
func TestAddForcePreservesExistingRoutes(t *testing.T) {
	cfg := addEnv(t)
	if _, err := Add(cfg, AddSpec{Domain: "app.test", Port: "8080"}); err != nil {
		t.Fatal(err)
	}
	if err := AddRoute("app-test", site.Route{ID: "api", Path: "/api", Upstream: site.Upstream{Kind: "localhost", Port: 3000}}); err != nil {
		t.Fatal(err)
	}

	if _, err := Add(cfg, AddSpec{Domain: "app.test", Port: "9090", Force: true}); err != nil {
		t.Fatal(err)
	}
	meta, _ := Read("app-test")
	if meta == nil || len(meta.Routes) != 1 || meta.Routes[0].ID != "api" {
		t.Errorf("forced Add() dropped the attached routes: %+v", meta)
	}
}

func TestAddRejectsInvalidSpecs(t *testing.T) {
	cfg := addEnv(t)
	cases := map[string]AddSpec{
		"neither port nor container": {Domain: "app.test"},
		"both":                       {Domain: "app.test", Port: "8080", Container: "c:1"},
		"bad domain":                 {Domain: "bad domain", Port: "8080"},
		"bad port":                   {Domain: "app.test", Port: "99999"},
		"bad explicit name":          {Name: "bad name", Domain: "app.test", Port: "8080"},
	}
	for name, spec := range cases {
		if _, err := Add(cfg, spec); err == nil {
			t.Errorf("%s: Add() = nil, want an error", name)
		}
	}
}

// A cert failure is fatal: serving the domain without TLS is not a degraded
// mode srv offers, so Add must stop rather than warn.
func TestAddFailsWhenCertGenerationFails(t *testing.T) {
	cfg := addEnv(t)
	t.Cleanup(mkcert.SwapRunner(mkcertStub{outErr: errors.New("mkcert exploded")}))

	if _, err := Add(cfg, AddSpec{Domain: "app.test", Port: "8080"}); err == nil {
		t.Fatal("Add() = nil, want the cert failure to be fatal")
	}
	if _, err := os.Stat(proxyConfigPath(cfg, "app-test")); err == nil {
		t.Error("Add() wrote a Traefik config despite failing to issue a cert")
	}
}

// ─── RemoveProxy ─────────────────────────────────────────────────────────

func TestRemoveProxyDeletesConfigAndMetadata(t *testing.T) {
	cfg := addEnv(t)
	if _, err := Add(cfg, AddSpec{Domain: "app.test", Port: "8080"}); err != nil {
		t.Fatal(err)
	}

	if _, err := RemoveProxy(cfg, "app-test"); err != nil {
		t.Fatalf("RemoveProxy() = %v", err)
	}
	if _, err := os.Stat(proxyConfigPath(cfg, "app-test")); !os.IsNotExist(err) {
		t.Error("Traefik proxy config survived removal")
	}
	meta, _ := Read("app-test")
	if meta != nil {
		t.Errorf("metadata sidecar survived removal: %+v", meta)
	}
}

func TestRemoveProxyUnknownName(t *testing.T) {
	cfg := addEnv(t)
	_, err := RemoveProxy(cfg, "ghost")
	if err == nil {
		t.Fatal("RemoveProxy(ghost) = nil, want a not-found error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v, want a not-found error", err)
	}
}

// Add → Remove → Add must work: leftover state from the first proxy would make
// the second Add refuse with "already exists".
func TestAddRemoveAddRoundTrip(t *testing.T) {
	cfg := addEnv(t)
	for i := range 2 {
		if _, err := Add(cfg, AddSpec{Domain: "app.test", Port: "8080"}); err != nil {
			t.Fatalf("Add() round %d = %v", i, err)
		}
		if _, err := RemoveProxy(cfg, "app-test"); err != nil {
			t.Fatalf("RemoveProxy() round %d = %v", i, err)
		}
	}
}

// ─── certSiteName ────────────────────────────────────────────────────────

// The synthetic cert site name keeps proxy certs from colliding with a real
// site of the same name, and must stay path-safe since it is joined into the
// certs directory.
func TestCertSiteNameIsDistinctAndPathSafe(t *testing.T) {
	got := certSiteName("app-test")
	if got == "app-test" {
		t.Error("certSiteName() must not collide with a real site name")
	}
	if !strings.HasPrefix(got, "_proxy-") {
		t.Errorf("certSiteName() = %q, want the _proxy- prefix", got)
	}
	if strings.ContainsAny(got, `/\`) || strings.Contains(got, "..") {
		t.Errorf("certSiteName() = %q, want a path-safe name", got)
	}
}
