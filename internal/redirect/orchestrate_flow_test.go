package redirect

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
)

// mkcertStub stands in for the mkcert binary. Output is the method cert
// generation goes through (mkcert.RunQuiet), so it is the one a failure test
// drives.
type mkcertStub struct{ outErr error }

func (m mkcertStub) Stream(...string) error { return nil }
func (m mkcertStub) Output(...string) ([]byte, error) {
	return []byte("/root/mkcert\n"), m.outErr
}

func (m mkcertStub) Combined(...string) ([]byte, error) {
	return []byte("Created a new local CA"), nil
}

func addEnv(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("SRV_ROOT", t.TempDir())
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

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.TraefikConfDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func redirectPath(cfg *config.Config, name string) string {
	return filepath.Join(cfg.TraefikConfDir(), constants.RedirectConfigPrefix+name+constants.ExtYAML)
}

// ─── HTTP redirects ──────────────────────────────────────────────────────

func TestAddHTTPRedirectWritesConfig(t *testing.T) {
	cfg := addEnv(t)

	res, err := Add(cfg, AddSpec{Domain: "old.test", To: "https://new.test"})
	if err != nil {
		t.Fatalf("Add() = %v", err)
	}
	if res.Name != "old-test" {
		t.Errorf("name = %q, want old-test (derived from the domain)", res.Name)
	}
	if res.DNSOnly {
		t.Error("a URL target must produce an HTTP redirect, not a DNS alias")
	}
	if _, err := os.Stat(redirectPath(cfg, "old-test")); err != nil {
		t.Errorf("redirect config not written: %v", err)
	}
}

// The written file has to be readable by ReadInfo, since that is what remove
// and reload depend on to find the domain again.
func TestAddHTTPRedirectIsReadableByReadInfo(t *testing.T) {
	cfg := addEnv(t)
	if _, err := Add(cfg, AddSpec{Domain: "old.test", To: "https://new.test"}); err != nil {
		t.Fatal(err)
	}
	info := ReadInfo(cfg, "old-test")
	if info.DNSOnly {
		t.Error("ReadInfo reported DNSOnly for an HTTP redirect")
	}
	if info.Domain != "old.test" {
		t.Errorf("ReadInfo domain = %q, want old.test", info.Domain)
	}
}

func TestAddPermanentAndWildcard(t *testing.T) {
	cfg := addEnv(t)
	if _, err := Add(cfg, AddSpec{Domain: "old.test", To: "https://new.test", Permanent: true, Wildcard: true}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(redirectPath(cfg, "old-test"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "301") {
		t.Errorf("permanent redirect did not render a 301:\n%s", body)
	}
	if !strings.Contains(body, "HostRegexp") {
		t.Errorf("wildcard did not render a subdomain matcher:\n%s", body)
	}
}

func TestAddTemporaryRendersA302(t *testing.T) {
	cfg := addEnv(t)
	if _, err := Add(cfg, AddSpec{Domain: "old.test", To: "https://new.test"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(redirectPath(cfg, "old-test"))
	if strings.Contains(string(data), "301") {
		t.Errorf("default redirect should be temporary (302):\n%s", data)
	}
}

// Serving a redirect for a domain srv has no cert for would break TLS, so a
// cert failure is fatal rather than a warning.
func TestAddHTTPFailsWhenCertGenerationFails(t *testing.T) {
	cfg := addEnv(t)
	t.Cleanup(mkcert.SwapRunner(mkcertStub{outErr: errors.New("mkcert exploded")}))

	if _, err := Add(cfg, AddSpec{Domain: "old.test", To: "https://new.test"}); err == nil {
		t.Fatal("Add() = nil, want the cert failure to be fatal")
	}
	if _, err := os.Stat(redirectPath(cfg, "old-test")); err == nil {
		t.Error("Add() wrote a redirect config despite failing to issue a cert")
	}
}

// ─── DNS-only aliases ────────────────────────────────────────────────────

// A DNS-only alias never terminates TLS, so it must not require mkcert at all.
func TestAddDNSOnlySkipsCertIssuance(t *testing.T) {
	cfg := addEnv(t)
	t.Cleanup(mkcert.SwapRunner(mkcertStub{outErr: errors.New("mkcert must not be called")}))

	res, err := Add(cfg, AddSpec{Domain: "alias.test", To: "target.test", DNSOnly: true})
	if err != nil {
		t.Fatalf("DNS-only Add() = %v", err)
	}
	if !res.DNSOnly {
		t.Error("result should be marked DNSOnly")
	}
	info := ReadInfo(cfg, "alias-test")
	if !info.DNSOnly || info.Domain != "alias.test" {
		t.Errorf("ReadInfo = %+v, want a DNS-only alias for alias.test", info)
	}
}

// An unresolvable target is a warning, not a failure: dnsmasq picks it up once
// the name starts resolving.
func TestAddDNSOnlyWarnsOnUnresolvableTarget(t *testing.T) {
	cfg := addEnv(t)
	res, err := Add(cfg, AddSpec{
		Domain:  "alias.test",
		To:      "definitely-not-a-real-host.invalid",
		DNSOnly: true,
	})
	if err != nil {
		t.Fatalf("Add() = %v, want a warning rather than an error", err)
	}
	if len(res.Warnings) == 0 {
		t.Error("expected a warning about the unresolvable target")
	}
}

// ─── guards shared by both kinds ─────────────────────────────────────────

func TestAddRefusesExistingRedirectWithoutForce(t *testing.T) {
	cfg := addEnv(t)
	if _, err := Add(cfg, AddSpec{Domain: "old.test", To: "https://new.test"}); err != nil {
		t.Fatal(err)
	}
	_, err := Add(cfg, AddSpec{Domain: "old.test", To: "https://other.test"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("second Add() = %v, want an 'already exists' refusal", err)
	}
}

func TestAddForceOverwrites(t *testing.T) {
	cfg := addEnv(t)
	if _, err := Add(cfg, AddSpec{Domain: "old.test", To: "https://new.test"}); err != nil {
		t.Fatal(err)
	}
	res, err := Add(cfg, AddSpec{Domain: "old.test", To: "https://other.test", Force: true})
	if err != nil {
		t.Fatalf("forced Add() = %v", err)
	}
	if !strings.Contains(res.Target, "other.test") {
		t.Errorf("target = %q, want the new target", res.Target)
	}
}

func TestAddRejectsInvalidSpecs(t *testing.T) {
	cfg := addEnv(t)
	cases := map[string]AddSpec{
		"bad domain":                 {Domain: "bad domain", To: "https://new.test"},
		"empty to":                   {Domain: "old.test", To: ""},
		"blank to":                   {Domain: "old.test", To: "   "},
		"bad name":                   {Name: "bad name", Domain: "old.test", To: "https://new.test"},
		"dns_only with a scheme":     {Domain: "old.test", To: "https://new.test", DNSOnly: true},
		"dns_only with a path":       {Domain: "old.test", To: "new.test/x", DNSOnly: true},
		"dns_only with wildcard":     {Domain: "old.test", To: "new.test", DNSOnly: true, Wildcard: true},
		"http target without scheme": {Domain: "old.test", To: "new.test"},
		"dns_only bad hostname":      {Domain: "old.test", To: "not a host", DNSOnly: true},
	}
	for name, spec := range cases {
		if _, err := Add(cfg, spec); err == nil {
			t.Errorf("%s: Add() = nil, want an error", name)
		}
	}
}

// ─── RemoveRedirect ──────────────────────────────────────────────────────

func TestRemoveRedirectHTTP(t *testing.T) {
	cfg := addEnv(t)
	if _, err := Add(cfg, AddSpec{Domain: "old.test", To: "https://new.test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveRedirect(cfg, "old-test"); err != nil {
		t.Fatalf("RemoveRedirect() = %v", err)
	}
	if _, err := os.Stat(redirectPath(cfg, "old-test")); !os.IsNotExist(err) {
		t.Error("redirect config survived removal")
	}
}

func TestRemoveRedirectDNSOnly(t *testing.T) {
	cfg := addEnv(t)
	if _, err := Add(cfg, AddSpec{Domain: "alias.test", To: "target.test", DNSOnly: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveRedirect(cfg, "alias-test"); err != nil {
		t.Fatalf("RemoveRedirect() = %v", err)
	}
	if _, err := os.Stat(redirectPath(cfg, "alias-test")); !os.IsNotExist(err) {
		t.Error("DNS-only redirect config survived removal")
	}
}

func TestRemoveRedirectUnknownName(t *testing.T) {
	cfg := addEnv(t)
	if _, err := RemoveRedirect(cfg, "ghost"); err == nil {
		t.Fatal("RemoveRedirect(ghost) = nil, want a not-found error")
	}
}

func TestAddRemoveAddRoundTrip(t *testing.T) {
	cfg := addEnv(t)
	for i := range 2 {
		if _, err := Add(cfg, AddSpec{Domain: "old.test", To: "https://new.test"}); err != nil {
			t.Fatalf("Add() round %d = %v", i, err)
		}
		if _, err := RemoveRedirect(cfg, "old-test"); err != nil {
			t.Fatalf("RemoveRedirect() round %d = %v", i, err)
		}
	}
}

// ─── ReadInfo / WriteDNSConfig ───────────────────────────────────────────

func TestReadInfoOnMissingOrGarbageFile(t *testing.T) {
	cfg := addEnv(t)
	if got := ReadInfo(cfg, "ghost"); got != (Info{}) {
		t.Errorf("ReadInfo(missing) = %+v, want the zero Info", got)
	}
	if err := os.WriteFile(redirectPath(cfg, "junk"), []byte("\t\nnot: [valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ReadInfo(cfg, "junk"); got != (Info{}) {
		t.Errorf("ReadInfo(garbage) = %+v, want the zero Info", got)
	}
}

func TestWriteDNSConfigCarriesSchemaHeader(t *testing.T) {
	cfg := addEnv(t)
	if err := WriteDNSConfig(cfg, "alias-test", "alias.test", "target.test"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(redirectPath(cfg, "alias-test"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "yaml-language-server: $schema=") {
		t.Errorf("DNS config lost its schema header:\n%s", body)
	}
	if !strings.Contains(body, "alias.test") || !strings.Contains(body, "target.test") {
		t.Errorf("DNS config missing source/target:\n%s", body)
	}
}

// certSiteName must not collide with a real site and must stay path-safe: it is
// joined into the certs directory.
func TestCertSiteNameIsDistinctAndPathSafe(t *testing.T) {
	got := certSiteName("old-test")
	if got == "old-test" {
		t.Error("certSiteName() must not collide with a real site name")
	}
	if strings.ContainsAny(got, `/\`) || strings.Contains(got, "..") {
		t.Errorf("certSiteName() = %q, want a path-safe name", got)
	}
}
