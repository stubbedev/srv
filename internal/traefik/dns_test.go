package traefik

import (
	"os"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
)

// setupDNSTestEnv points SRV_ROOT at a fresh temp directory, resets the
// config cache, and pre-creates the traefik subdirectory so that
// SaveLocalDomains and friends can write files without hitting "no such
// directory" errors.
// The returned function must be called at the end of the test (defer it).
func setupDNSTestEnv(t *testing.T) func() {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("SRV_ROOT", tmpDir)
	config.ResetCache()

	// Pre-create the traefik sub-directory that config.Load does not create.
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("setupDNSTestEnv: failed to load config: %v", err)
	}
	if err := os.MkdirAll(cfg.TraefikDir, 0o755); err != nil {
		t.Fatalf("setupDNSTestEnv: failed to create traefik dir: %v", err)
	}

	return func() {
		config.ResetCache()
	}
}

func TestLoadSaveLocalDomains(t *testing.T) {
	defer setupDNSTestEnv(t)()

	t.Run("load from non-existent file returns empty slice", func(t *testing.T) {
		domains, err := LoadLocalDomains()
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if len(domains) != 0 {
			t.Errorf("expected empty slice, got %v", domains)
		}
	})

	t.Run("save and load domains", func(t *testing.T) {
		testDomains := []string{"foo.test", "bar.test", "baz.local"}

		if err := SaveLocalDomains(testDomains); err != nil {
			t.Fatalf("failed to save domains: %v", err)
		}

		loaded, err := LoadLocalDomains()
		if err != nil {
			t.Fatalf("failed to load domains: %v", err)
		}

		if len(loaded) != len(testDomains) {
			t.Errorf("expected %d domains, got %d", len(testDomains), len(loaded))
		}

		// Domains should be sorted
		expected := []string{"bar.test", "baz.local", "foo.test"}
		for i, d := range loaded {
			if d != expected[i] {
				t.Errorf("expected domain[%d]=%s, got %s", i, expected[i], d)
			}
		}
	})

	t.Run("save deduplicates domains", func(t *testing.T) {
		testDomains := []string{"foo.test", "bar.test", "foo.test", "bar.test"}

		if err := SaveLocalDomains(testDomains); err != nil {
			t.Fatalf("failed to save domains: %v", err)
		}

		loaded, err := LoadLocalDomains()
		if err != nil {
			t.Fatalf("failed to load domains: %v", err)
		}

		if len(loaded) != 2 {
			t.Errorf("expected 2 unique domains, got %d: %v", len(loaded), loaded)
		}
	})

	t.Run("save empty list", func(t *testing.T) {
		if err := SaveLocalDomains([]string{}); err != nil {
			t.Fatalf("failed to save empty domains: %v", err)
		}

		loaded, err := LoadLocalDomains()
		if err != nil {
			t.Fatalf("failed to load domains: %v", err)
		}

		if len(loaded) != 0 {
			t.Errorf("expected 0 domains, got %d: %v", len(loaded), loaded)
		}
	})
}

func TestDomainRegistration(t *testing.T) {
	defer setupDNSTestEnv(t)()

	t.Run("register domain adds to empty list", func(t *testing.T) {
		// Pre-condition: no domains registered yet.
		if err := SaveLocalDomains([]string{}); err != nil {
			t.Fatal(err)
		}

		domains, err := LoadLocalDomains()
		if err != nil {
			t.Fatal(err)
		}
		if len(domains) != 0 {
			t.Fatalf("expected empty start, got %v", domains)
		}

		domains = append(domains, "api.test")
		if err := SaveLocalDomains(domains); err != nil {
			t.Fatalf("failed to save domains: %v", err)
		}

		loaded, err := LoadLocalDomains()
		if err != nil {
			t.Fatal(err)
		}
		if len(loaded) != 1 || loaded[0] != "api.test" {
			t.Errorf("expected [api.test], got %v", loaded)
		}
	})

	t.Run("save deduplicates (idempotent register)", func(t *testing.T) {
		// Add same domain a second time.
		domains, _ := LoadLocalDomains()
		domains = append(domains, "api.test") // duplicate
		if err := SaveLocalDomains(domains); err != nil {
			t.Fatal(err)
		}

		loaded, err := LoadLocalDomains()
		if err != nil {
			t.Fatal(err)
		}
		if len(loaded) != 1 {
			t.Errorf("expected 1 domain after re-registering, got %d: %v", len(loaded), loaded)
		}
	})

	t.Run("register multiple domains", func(t *testing.T) {
		domains, _ := LoadLocalDomains()
		domains = append(domains, "web.test")
		if err := SaveLocalDomains(domains); err != nil {
			t.Fatal(err)
		}

		loaded, _ := LoadLocalDomains()
		if len(loaded) != 2 {
			t.Errorf("expected 2 domains, got %d: %v", len(loaded), loaded)
		}
	})

	t.Run("unregister domain removes it", func(t *testing.T) {
		domains, _ := LoadLocalDomains()
		filtered := make([]string, 0, len(domains))
		for _, d := range domains {
			if d != "api.test" {
				filtered = append(filtered, d)
			}
		}
		if err := SaveLocalDomains(filtered); err != nil {
			t.Fatal(err)
		}

		loaded, _ := LoadLocalDomains()
		if len(loaded) != 1 || loaded[0] != "web.test" {
			t.Errorf("expected [web.test], got %v", loaded)
		}
	})

	t.Run("unregister last domain leaves empty list", func(t *testing.T) {
		if err := SaveLocalDomains([]string{}); err != nil {
			t.Fatal(err)
		}

		loaded, _ := LoadLocalDomains()
		if len(loaded) != 0 {
			t.Errorf("expected 0 domains, got %d: %v", len(loaded), loaded)
		}
	})
}

func TestGenerateDnsmasqConfig(t *testing.T) {
	defer setupDNSTestEnv(t)()

	// The dnsmasq config is rendered by two pure builders. They are tested
	// directly rather than through UpdateDnsmasqConfig, which also touches
	// docker — and would recreate a real DNS container if one happens to be
	// running on the machine executing the tests.
	t.Run("buildDnsmasqConf renders hostsdir and wildcards", func(t *testing.T) {
		conf := buildDnsmasqConf(nil, []string{"8.8.8.8", "8.8.4.4"})
		for _, want := range []string{
			"hostsdir=/etc/dnsmasq.hosts",
			"# No wildcard domains registered",
			"server=8.8.8.8",
			"server=8.8.4.4",
			"no-resolv",
		} {
			if !strings.Contains(conf, want) {
				t.Errorf("buildDnsmasqConf missing %q in:\n%s", want, conf)
			}
		}
		withWildcard := buildDnsmasqConf([]string{"foo.test"}, []string{"1.1.1.1"})
		if !strings.Contains(withWildcard, "address=/foo.test/127.0.0.1") {
			t.Errorf("buildDnsmasqConf missing wildcard directive in:\n%s", withWildcard)
		}
	})

	t.Run("buildDnsmasqHosts renders exact records and is never empty", func(t *testing.T) {
		// dnsmasq cannot detect an emptied hostsdir file, so the builder must
		// always emit a non-empty file even with no domains.
		if buildDnsmasqHosts(nil) == "" {
			t.Error("buildDnsmasqHosts(nil) must not be empty")
		}
		hosts := buildDnsmasqHosts([]string{"api.test", "web.test"})
		for _, want := range []string{"127.0.0.1 api.test", "127.0.0.1 web.test"} {
			if !strings.Contains(hosts, want) {
				t.Errorf("buildDnsmasqHosts missing %q in:\n%s", want, hosts)
			}
		}
	})

	t.Run("DnsmasqConf constant matches builder output", func(t *testing.T) {
		// EnsureConfig writes the DnsmasqConf constant on a fresh install;
		// if it drifts from the builder, the first domain add sees a spurious
		// config change and needlessly restarts the DNS container.
		want := buildDnsmasqConf(nil, []string{constants.GoogleDNS1, constants.GoogleDNS2})
		if DnsmasqConf != want {
			t.Errorf("DnsmasqConf drifted from buildDnsmasqConf:\n--- const ---\n%s\n--- builder ---\n%s", DnsmasqConf, want)
		}
	})

	t.Run("single domain appears in local-domains list", func(t *testing.T) {
		if err := SaveLocalDomains([]string{"api.test"}); err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadLocalDomains()
		if err != nil {
			t.Fatal(err)
		}
		if len(loaded) != 1 || loaded[0] != "api.test" {
			t.Errorf("expected [api.test], got %v", loaded)
		}
	})

	t.Run("load ignores comments and empty lines in saved file", func(t *testing.T) {
		// Manually write a file with comments to test LoadLocalDomains parsing.
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		domainsPath := cfg.TraefikDir + "/local-domains.txt"
		content := "# comment\nfoo.test\n\n# another\nbar.test\n\n"
		if err := os.WriteFile(domainsPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		loaded, err := LoadLocalDomains()
		if err != nil {
			t.Fatal(err)
		}
		if len(loaded) != 2 {
			t.Errorf("expected 2 domains, got %d: %v", len(loaded), loaded)
		}
	})
}
