package redirect

import (
	"os"
	"testing"

	"github.com/stubbedev/srv/internal/config"
)

// loadCfg loads config under the test's SRV_ROOT and ensures the Traefik conf
// dir exists so writes land.
func loadCfg(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if err := os.MkdirAll(cfg.TraefikConfDir(), 0o755); err != nil {
		t.Fatalf("mkdir conf dir: %v", err)
	}
	return cfg
}

func TestValidateAddSpecHTTP(t *testing.T) {
	// Positive: absolute https URL, name derived from domain.
	name, to, err := validateAddSpec(AddSpec{Domain: "old.test", To: "https://new.example.com/"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "old-test" {
		t.Errorf("derived name = %q, want old-test", name)
	}
	if to != "https://new.example.com" {
		t.Errorf("normalized to = %q (trailing slash should be trimmed)", to)
	}
}

func TestValidateAddSpecHTTPNegative(t *testing.T) {
	cases := []AddSpec{
		{Domain: "old.test", To: "new.example.com"},             // no scheme
		{Domain: "old.test", To: "ftp://x"},                     // wrong scheme
		{Domain: "bad/domain", To: "https://x.test"},            // invalid domain
		{Domain: "old.test", To: "https://x.test", Name: "x/y"}, // invalid name
	}
	for i, c := range cases {
		if _, _, err := validateAddSpec(c); err == nil {
			t.Errorf("case %d: expected error for %+v", i, c)
		}
	}
}

func TestValidateAddSpecDNSOnly(t *testing.T) {
	// Positive: bare hostname target.
	_, to, err := validateAddSpec(AddSpec{Domain: "old.test", To: "host.example.com", DNSOnly: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if to != "host.example.com" {
		t.Errorf("to = %q", to)
	}

	// Negative: scheme/path not allowed, wildcard not allowed.
	bad := []AddSpec{
		{Domain: "old.test", To: "https://x.test", DNSOnly: true},
		{Domain: "old.test", To: "x.test/path", DNSOnly: true},
		{Domain: "old.test", To: "x.test", DNSOnly: true, Wildcard: true},
		{Domain: "old.test", To: "evil.test/127.0.0.1", DNSOnly: true}, // injection-shaped target
	}
	for i, c := range bad {
		if _, _, err := validateAddSpec(c); err == nil {
			t.Errorf("case %d: expected error for %+v", i, c)
		}
	}
}

func TestReadInfoRoundTrip(t *testing.T) {
	t.Setenv("SRV_ROOT", t.TempDir())
	cfg := loadCfg(t)

	if err := WriteDNSConfig(cfg, "alias", "old.test", "new.test"); err != nil {
		t.Fatal(err)
	}
	info := ReadInfo(cfg, "alias")
	if !info.DNSOnly {
		t.Error("expected DNSOnly=true for a DNS alias file")
	}
	if info.Domain != "old.test" {
		t.Errorf("domain = %q, want old.test", info.Domain)
	}

	// A missing redirect yields a zero Info, not a panic.
	if got := ReadInfo(cfg, "nope"); got.DNSOnly || got.Domain != "" {
		t.Errorf("missing redirect should yield zero Info, got %+v", got)
	}
}
