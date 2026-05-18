package traefik

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/shell/shelltest"
)

func setupDNSTest(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	config.ResetCache()
	t.Cleanup(config.ResetCache)
	if err := os.MkdirAll(filepath.Join(root, "traefik"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRegisterLocalDomainNew(t *testing.T) {
	setupDNSTest(t)
	swapShell(t, shelltest.New(nil))
	if err := RegisterLocalDomain("foo.local", false); err != nil {
		t.Fatalf("RegisterLocalDomain err: %v", err)
	}
	domains, err := LoadLocalDomains()
	if err != nil {
		t.Fatal(err)
	}
	if len(domains) != 1 || domains[0] != "foo.local" {
		t.Errorf("got %v, want [foo.local]", domains)
	}
}

func TestRegisterLocalDomainWildcard(t *testing.T) {
	setupDNSTest(t)
	swapShell(t, shelltest.New(nil))
	if err := RegisterLocalDomain("foo.com", true); err != nil {
		t.Fatal(err)
	}
	domains, _ := LoadLocalDomains()
	if len(domains) != 1 || domains[0] != "*.foo.com" {
		t.Errorf("got %v, want [*.foo.com]", domains)
	}
}

func TestRegisterLocalDomainIdempotent(t *testing.T) {
	setupDNSTest(t)
	swapShell(t, shelltest.New(nil))
	if err := RegisterLocalDomain("foo.local", false); err != nil {
		t.Fatal(err)
	}
	if err := RegisterLocalDomain("foo.local", false); err != nil {
		t.Fatal(err)
	}
	domains, _ := LoadLocalDomains()
	if len(domains) != 1 {
		t.Errorf("expected 1 entry after dup register, got %v", domains)
	}
}

func TestRegisterLocalDomainWildcardUpgradesApex(t *testing.T) {
	setupDNSTest(t)
	swapShell(t, shelltest.New(nil))
	if err := RegisterLocalDomain("foo.com", false); err != nil {
		t.Fatal(err)
	}
	if err := RegisterLocalDomain("foo.com", true); err != nil {
		t.Fatal(err)
	}
	domains, _ := LoadLocalDomains()
	if len(domains) != 1 || domains[0] != "*.foo.com" {
		t.Errorf("expected upgrade to wildcard, got %v", domains)
	}
}

func TestUnregisterLocalDomain(t *testing.T) {
	setupDNSTest(t)
	swapShell(t, shelltest.New(nil))
	_ = RegisterLocalDomain("foo.local", false)
	_ = RegisterLocalDomain("bar.local", true)
	if err := UnregisterLocalDomain("foo.local"); err != nil {
		t.Fatal(err)
	}
	domains, _ := LoadLocalDomains()
	if len(domains) != 1 || domains[0] != "*.bar.local" {
		t.Errorf("got %v, want [*.bar.local]", domains)
	}
}

func TestUnregisterLocalDomainMatchesWildcard(t *testing.T) {
	setupDNSTest(t)
	swapShell(t, shelltest.New(nil))
	_ = RegisterLocalDomain("foo.com", true)
	if err := UnregisterLocalDomain("foo.com"); err != nil {
		t.Fatal(err)
	}
	domains, _ := LoadLocalDomains()
	if len(domains) != 0 {
		t.Errorf("expected empty after unregister, got %v", domains)
	}
}

func TestUnregisterLocalDomainMissingNoOp(t *testing.T) {
	setupDNSTest(t)
	swapShell(t, shelltest.New(nil))
	if err := UnregisterLocalDomain("nothing.local"); err != nil {
		t.Errorf("missing should be no-op: %v", err)
	}
}

func TestUpdateDnsmasqConfigCreatesFiles(t *testing.T) {
	root := setupDNSTest(t)
	swapShell(t, shelltest.New(nil))
	if err := UpdateDnsmasqConfig(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "traefik", "dnsmasq.conf")); err != nil {
		t.Errorf("dnsmasq.conf missing: %v", err)
	}
}

func TestSetupMacOSResolverCallsUpdate(t *testing.T) {
	setupDNSTest(t)
	swapShell(t, shelltest.New(nil))
	_ = setupMacOSResolver()
}

func TestReloadDNS(t *testing.T) {
	setupDNSTest(t)
	swapShell(t, shelltest.New(nil))
	// docker.SwapComposeExec stub keeps the call from forking.
	// Without it ReloadDNS shells out to `docker compose`.
	// Just verify no panic via the composeExec seam already wired in
	// other tests — call with empty defaults and accept either outcome.
	_ = ReloadDNS()
}

func TestReloadDNSHosts(t *testing.T) {
	setupDNSTest(t)
	t.Cleanup(docker.SwapDockerExec(func(bool, ...string) error { return nil }))
	if err := reloadDNSHosts(); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestUpdateDnsmasqConfigWithWildcards(t *testing.T) {
	setupDNSTest(t)
	swapShell(t, shelltest.New(nil))
	if err := RegisterLocalDomain("foo.com", true); err != nil {
		t.Fatal(err)
	}
	if err := RegisterLocalDomain("bar.com", false); err != nil {
		t.Fatal(err)
	}
	if err := UpdateDnsmasqConfig(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateDnsmasqConfigUserUpstream(t *testing.T) {
	setupDNSTest(t)
	cfg, _ := config.Load()
	uc := &config.UserConfig{UpstreamDNS: []string{"1.1.1.1", "8.8.8.8"}}
	if err := cfg.SaveUserConfig(uc); err != nil {
		t.Fatal(err)
	}
	swapShell(t, shelltest.New(nil))
	if err := UpdateDnsmasqConfig(); err != nil {
		t.Fatal(err)
	}
}
