package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/config"
)

// ListBasic carries the parsed metadata so list consumers (doctor, validate,
// install) do not re-read and re-parse every file.
func TestListBasicCarriesMetadata(t *testing.T) {
	withSRVRoot(t)
	seedSite(t, "blog", []string{"blog.test"})

	sites, err := ListBasic()
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 {
		t.Fatalf("sites = %d, want 1", len(sites))
	}
	meta := sites[0].Metadata()
	if meta == nil {
		t.Fatal("Metadata() nil after ListBasic")
	}
	if len(meta.Domains) != 1 || meta.Domains[0] != "blog.test" {
		t.Errorf("carried metadata domains = %v", meta.Domains)
	}
	if sites[0].Type != meta.Type {
		t.Errorf("carried metadata disagrees with the site: %v vs %v", meta.Type, sites[0].Type)
	}
}

// ForceReloadWithoutDNS defers the DNS pipeline to a batched call: no
// registry write happens per site, while the site's own artifacts (cert,
// routes) still refresh.
func TestForceReloadWithoutDNSDefersRegistration(t *testing.T) {
	withSRVRoot(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.TraefikDir, 0o755); err != nil {
		t.Fatal(err)
	}
	seedSite(t, "d", []string{"d.test"})

	res, err := ForceReloadWithoutDNS("d")
	if err != nil {
		t.Fatalf("ForceReloadWithoutDNS: %v", err)
	}
	if res.DNSRegistered != 0 {
		t.Errorf("DNSRegistered = %d, want 0 (deferred to the batch call)", res.DNSRegistered)
	}
	if _, err := os.Stat(filepath.Join(cfg.TraefikDir, "local-domains.txt")); !os.IsNotExist(err) {
		t.Errorf("per-site reload wrote the DNS registry despite skipDNS (stat err = %v)", err)
	}
}
