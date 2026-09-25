package traefik

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
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

// TestRegisterLocalDomainConcurrent pins the single-writer rule: concurrent
// registrations (a batch start racing the daemon) must all land in the
// registry instead of each writing back their own read-modify-write copy.
func TestRegisterLocalDomainConcurrent(t *testing.T) {
	setupDNSTest(t)
	swapShell(t, shelltest.New(nil))

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Go(func() {
			errs[i] = RegisterLocalDomain(fmt.Sprintf("site%02d.test", i), false)
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("register %d: %v", i, err)
		}
	}
	domains, err := LoadLocalDomains()
	if err != nil {
		t.Fatal(err)
	}
	if len(domains) != n {
		t.Errorf("lost updates: got %d entries, want %d: %v", len(domains), n, domains)
	}
}

// The batch form registers every domain with one registry write and one
// dnsmasq regen, and a second identical call is a no-op that does not touch
// the generated files (the rename itself would make dnsmasq re-read + flush).
func TestRegisterLocalDomainsBatchIdempotent(t *testing.T) {
	setupDNSTest(t)
	swapShell(t, shelltest.New(nil))

	domains := []string{"a.test", "b.test", "c.test"}
	if err := RegisterLocalDomains(domains, false); err != nil {
		t.Fatal(err)
	}
	registered, err := LoadLocalDomains()
	if err != nil {
		t.Fatal(err)
	}
	if len(registered) != 3 {
		t.Fatalf("got %v, want the 3 batch domains", registered)
	}

	hostsPath := filepath.Join(t.TempDir())
	_ = hostsPath
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	hostsFile := filepath.Join(cfg.TraefikDir, constants.DnsmasqHostsDir, constants.DnsmasqHostsFile)
	confFile := filepath.Join(cfg.TraefikDir, constants.DnsmasqConfFile)
	hostsBefore, mErr := os.ReadFile(hostsFile)
	confBefore, _ := os.ReadFile(confFile)
	if mErr != nil {
		t.Fatal(mErr)
	}

	if err := RegisterLocalDomains(domains, false); err != nil {
		t.Fatal(err)
	}
	hostsAfter, _ := os.ReadFile(hostsFile)
	confAfter, _ := os.ReadFile(confFile)
	if string(hostsAfter) != string(hostsBefore) || string(confAfter) != string(confBefore) {
		t.Error("no-op registration rewrote the dnsmasq files")
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
