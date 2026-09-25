package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/redirect"
	"github.com/stubbedev/srv/internal/traefik"
)

// setupRedirectTestEnv points config at a fresh temp dir and pre-creates the
// traefik conf subdir so writeRedirect*Config calls land somewhere real. It
// snapshots the global redirect flags so test cases can mutate them in
// isolation and have the original restored on cleanup.
func setupRedirectTestEnv(t *testing.T) *config.Config {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("SRV_ROOT", tmpDir)
	config.ResetCache()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if err := os.MkdirAll(cfg.TraefikConfDir(), 0o755); err != nil {
		t.Fatalf("mkdir traefikconfdir: %v", err)
	}
	if err := os.MkdirAll(cfg.SitesDir, 0o755); err != nil {
		t.Fatalf("mkdir sitesdir: %v", err)
	}

	saved := redirectAddFlags
	t.Cleanup(func() {
		redirectAddFlags = saved
		config.ResetCache()
	})
	return cfg
}

// =============================================================================
// WriteRedirectConfig (HTTP)
// =============================================================================

func TestWriteRedirectConfig_HTTP(t *testing.T) {
	cfg := setupRedirectTestEnv(t)

	input := traefik.HTTPRedirect{
		Name:      "old-test",
		Domain:    "old.test",
		To:        "https://new.test",
		Permanent: true,
		Wildcard:  false,
	}
	if err := traefik.WriteRedirectConfig(cfg, input); err != nil {
		t.Fatalf("WriteRedirectConfig: %v", err)
	}

	path := filepath.Join(cfg.TraefikConfDir(), constants.RedirectConfigPrefix+input.Name+constants.ExtYAML)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	content := string(data)

	wants := []string{
		"http:",                            // Traefik schema
		"redirect-old-test:",               // router key
		"Host(`old.test`)",                 // exact host rule
		"- websecure",                      // HTTPS entrypoint
		"- web",                            // HTTP entrypoint
		"tls: {}",                          // TLS termination
		"redirect-old-test-mw",             // middleware ref
		"redirectRegex:",                   // middleware type
		"replacement: https://new.test/$1", // target appended
		"permanent: true",                  // 301
		"http://127.0.0.1:1",               // black-hole noop service
	}
	for _, w := range wants {
		if !strings.Contains(content, w) {
			t.Errorf("missing %q in:\n%s", w, content)
		}
	}
}

func TestWriteRedirectConfig_HTTPWildcardAndTemporary(t *testing.T) {
	cfg := setupRedirectTestEnv(t)

	input := traefik.HTTPRedirect{
		Name:      "old-test",
		Domain:    "old.test",
		To:        "https://new.test",
		Permanent: false, // 302
		Wildcard:  true,
	}
	if err := traefik.WriteRedirectConfig(cfg, input); err != nil {
		t.Fatalf("WriteRedirectConfig: %v", err)
	}

	path := filepath.Join(cfg.TraefikConfDir(), constants.RedirectConfigPrefix+input.Name+constants.ExtYAML)
	data, _ := os.ReadFile(path)
	content := string(data)

	if !strings.Contains(content, "HostRegexp(") {
		t.Errorf("wildcard must emit HostRegexp matcher:\n%s", content)
	}
	if !strings.Contains(content, "permanent: false") {
		t.Errorf("temporary must serialize as permanent:false:\n%s", content)
	}
}

// =============================================================================
// redirect.WriteDNSConfig (DNS-only)
// =============================================================================

func TestWriteRedirectDNSConfig(t *testing.T) {
	cfg := setupRedirectTestEnv(t)

	input := traefik.HTTPRedirect{
		Name:   "alias",
		Domain: "old.test",
		To:     "new.example.com",
	}
	if err := redirect.WriteDNSConfig(cfg, input.Name, input.Domain, input.To); err != nil {
		t.Fatalf("WriteDNSConfig: %v", err)
	}

	path := filepath.Join(cfg.TraefikConfDir(), constants.RedirectConfigPrefix+input.Name+constants.ExtYAML)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	content := string(data)

	wants := []string{
		"# yaml-language-server: $schema=" + constants.RedirectDNSSchemaURL,
		"dns:",
		"source: old.test",
		"target: new.example.com",
	}
	for _, w := range wants {
		if !strings.Contains(content, w) {
			t.Errorf("missing %q in:\n%s", w, content)
		}
	}
	if !strings.HasPrefix(content, "# yaml-language-server:") {
		t.Errorf("schema modeline must be the first line; got:\n%s", content)
	}
	// DNS-only files must NOT carry any Traefik schema — that would cause
	// the file provider to instantiate a real HTTP router.
	for _, forbidden := range []string{"http:", "routers:", "redirectRegex"} {
		if strings.Contains(content, forbidden) {
			t.Errorf("DNS-only yaml leaked %q (must be metadata-only):\n%s", forbidden, content)
		}
	}
}

// =============================================================================
// readRedirectConfig
// =============================================================================

func TestReadRedirectConfig_HTTPRoundTrip(t *testing.T) {
	cfg := setupRedirectTestEnv(t)

	input := traefik.HTTPRedirect{
		Name:      "old-test",
		Domain:    "old.test",
		To:        "https://new.test",
		Permanent: true,
	}
	if err := traefik.WriteRedirectConfig(cfg, input); err != nil {
		t.Fatalf("WriteRedirectConfig: %v", err)
	}
	info := readRedirectConfig(cfg, input.Name)

	if info.DNSOnly {
		t.Errorf("HTTP redirect read back as DNSOnly")
	}
	if info.Domain != "old.test" {
		t.Errorf("domain: got %q, want %q", info.Domain, "old.test")
	}
	if info.Target != "https://new.test" {
		t.Errorf("target: got %q, want %q", info.Target, "https://new.test")
	}
	if !info.Permanent {
		t.Errorf("permanent: got false, want true")
	}
}

func TestReadRedirectConfig_DNSRoundTrip(t *testing.T) {
	cfg := setupRedirectTestEnv(t)

	input := traefik.HTTPRedirect{
		Name:   "alias",
		Domain: "old.test",
		To:     "new.example.com",
	}
	if err := redirect.WriteDNSConfig(cfg, input.Name, input.Domain, input.To); err != nil {
		t.Fatalf("WriteDNSConfig: %v", err)
	}
	info := readRedirectConfig(cfg, input.Name)

	if !info.DNSOnly {
		t.Errorf("DNS redirect read back as HTTP")
	}
	if info.Domain != "old.test" {
		t.Errorf("domain: got %q, want %q", info.Domain, "old.test")
	}
	if info.Target != "new.example.com" {
		t.Errorf("target: got %q, want %q", info.Target, "new.example.com")
	}
}

func TestReadRedirectConfig_MissingFile(t *testing.T) {
	cfg := setupRedirectTestEnv(t)
	info := readRedirectConfig(cfg, "does-not-exist")
	if info.Target != "unknown" {
		t.Errorf("missing file should yield Target=unknown sentinel, got %q", info.Target)
	}
}

// =============================================================================
// getRedirectNames
// =============================================================================

// TestDNSContractAcrossPackages enforces the schema contract between the
// internal/redirect writer (WriteDNSConfig) and the traefik-side scanner
// (ScanRedirectAliases). Renaming `source` or `target` on one side without
// updating the other would silently break the dnsmasq regen pipeline; this
// test makes the regression loud.
func TestDNSContractAcrossPackages(t *testing.T) {
	cfg := setupRedirectTestEnv(t)

	input := traefik.HTTPRedirect{
		Name:   "alias",
		Domain: "old.test",
		To:     "new.example.com",
	}
	if err := redirect.WriteDNSConfig(cfg, input.Name, input.Domain, input.To); err != nil {
		t.Fatalf("WriteDNSConfig: %v", err)
	}

	aliases, err := traefik.ScanRedirectAliases()
	if err != nil {
		t.Fatalf("ScanRedirectAliases: %v", err)
	}
	if len(aliases) != 1 {
		t.Fatalf("expected 1 alias parsed back, got %d (%+v)", len(aliases), aliases)
	}
	if aliases[0].Source != "old.test" || aliases[0].Target != "new.example.com" || aliases[0].Name != "alias" {
		t.Errorf("contract broken — file written by cmd not parsed by traefik scanner: got %+v", aliases[0])
	}
}

func TestGetRedirectNames(t *testing.T) {
	cfg := setupRedirectTestEnv(t)

	// Write a mix of HTTP, DNS, and unrelated files. Only redirect-*.yml
	// should be discovered, regardless of which schema is inside.
	if err := traefik.WriteRedirectConfig(cfg, traefik.HTTPRedirect{Name: "alpha", Domain: "alpha.test", To: "https://x.test", Permanent: true}); err != nil {
		t.Fatal(err)
	}
	if err := redirect.WriteDNSConfig(cfg, "beta", "beta.test", "y.test"); err != nil {
		t.Fatal(err)
	}
	// Decoy files that must be ignored.
	for _, name := range []string{"proxy-foo.yml", "site-bar.yml", "redirect-bad.txt"} {
		if err := os.WriteFile(filepath.Join(cfg.TraefikConfDir(), name), []byte("noop"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	names := getRedirectNames()
	want := map[string]bool{"alpha": true, "beta": true}
	if len(names) != len(want) {
		t.Errorf("got %d names, want %d: %v", len(names), len(want), names)
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected entry %q in names list", n)
		}
	}
}
