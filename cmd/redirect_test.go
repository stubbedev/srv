package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
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
// validateRedirectInput
// =============================================================================

func TestValidateRedirectInput_HTTPMode(t *testing.T) {
	tests := []struct {
		name      string
		domain    string
		to        string
		temporary bool
		wildcard  bool
		flagName  string
		wantErr   string // substring to match; "" means success
		wantTo    string
		wantPerm  bool
		wantWild  bool
		wantName  string
	}{
		{
			name:     "valid https target",
			domain:   "old.test",
			to:       "https://new.example.com",
			wantTo:   "https://new.example.com",
			wantPerm: true,
			wantName: "old-test",
		},
		{
			name:     "valid http target",
			domain:   "old.test",
			to:       "http://internal.example",
			wantTo:   "http://internal.example",
			wantPerm: true,
			wantName: "old-test",
		},
		{
			name:     "trailing slash stripped",
			domain:   "old.test",
			to:       "https://new.test/",
			wantTo:   "https://new.test",
			wantPerm: true,
			wantName: "old-test",
		},
		{
			name:      "temporary flips permanent off",
			domain:    "old.test",
			to:        "https://new.test",
			temporary: true,
			wantTo:    "https://new.test",
			wantPerm:  false,
			wantName:  "old-test",
		},
		{
			name:     "wildcard preserved",
			domain:   "old.test",
			to:       "https://new.test",
			wildcard: true,
			wantTo:   "https://new.test",
			wantPerm: true,
			wantWild: true,
			wantName: "old-test",
		},
		{
			name:     "custom name overrides derived",
			domain:   "old.test",
			to:       "https://new.test",
			flagName: "custom-name",
			wantTo:   "https://new.test",
			wantPerm: true,
			wantName: "custom-name",
		},

		// rejections
		{
			name:    "rejects ftp scheme",
			domain:  "old.test",
			to:      "ftp://bad",
			wantErr: "scheme must be http or https",
		},
		{
			name:    "rejects relative url",
			domain:  "old.test",
			to:      "/no-host",
			wantErr: "must be an absolute",
		},
		{
			name:    "rejects empty target",
			domain:  "old.test",
			to:      "",
			wantErr: "must be an absolute",
		},
		{
			name:    "rejects invalid domain",
			domain:  "_bad domain_",
			to:      "https://new.test",
			wantErr: "invalid domain",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setupRedirectTestEnv(t)
			redirectAddFlags.domain = tc.domain
			redirectAddFlags.to = tc.to
			redirectAddFlags.temporary = tc.temporary
			redirectAddFlags.permanent = !tc.temporary
			redirectAddFlags.wildcard = tc.wildcard
			redirectAddFlags.name = tc.flagName
			redirectAddFlags.dnsOnly = false

			got, err := validateRedirectInput()
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got success: %+v", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("expected error containing %q, got %q", tc.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.to != tc.wantTo {
				t.Errorf("to: got %q, want %q", got.to, tc.wantTo)
			}
			if got.permanent != tc.wantPerm {
				t.Errorf("permanent: got %v, want %v", got.permanent, tc.wantPerm)
			}
			if got.wildcard != tc.wantWild {
				t.Errorf("wildcard: got %v, want %v", got.wildcard, tc.wantWild)
			}
			if got.name != tc.wantName {
				t.Errorf("name: got %q, want %q", got.name, tc.wantName)
			}
			if got.dnsOnly {
				t.Errorf("dnsOnly: want false, got true")
			}
		})
	}
}

func TestValidateRedirectInput_DNSOnlyMode(t *testing.T) {
	tests := []struct {
		name      string
		domain    string
		to        string
		temporary bool
		wildcard  bool
		wantErr   string
		wantTo    string
	}{
		{
			name:   "valid bare hostname",
			domain: "old.test",
			to:     "new.example.com",
			wantTo: "new.example.com",
		},
		{
			name:    "rejects scheme",
			domain:  "old.test",
			to:      "https://new.test",
			wantErr: "bare hostname",
		},
		{
			name:    "rejects path",
			domain:  "old.test",
			to:      "new.test/path",
			wantErr: "bare hostname",
		},
		{
			name:    "rejects query",
			domain:  "old.test",
			to:      "new.test?q=1",
			wantErr: "bare hostname",
		},
		{
			name:    "rejects fragment",
			domain:  "old.test",
			to:      "new.test#frag",
			wantErr: "bare hostname",
		},
		{
			name:     "rejects --wildcard",
			domain:   "old.test",
			to:       "new.test",
			wildcard: true,
			wantErr:  "wildcard is not supported",
		},
		{
			name:      "rejects --temporary",
			domain:    "old.test",
			to:        "new.test",
			temporary: true,
			wantErr:   "temporary is not supported",
		},
		{
			name:    "rejects malformed target hostname",
			domain:  "old.test",
			to:      "_bad_",
			wantErr: "invalid --to hostname",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setupRedirectTestEnv(t)
			redirectAddFlags.domain = tc.domain
			redirectAddFlags.to = tc.to
			redirectAddFlags.temporary = tc.temporary
			redirectAddFlags.permanent = !tc.temporary
			redirectAddFlags.wildcard = tc.wildcard
			redirectAddFlags.dnsOnly = true

			got, err := validateRedirectInput()
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got success: %+v", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("expected error containing %q, got %q", tc.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.dnsOnly {
				t.Errorf("dnsOnly: want true, got false")
			}
			if got.to != tc.wantTo {
				t.Errorf("to: got %q, want %q", got.to, tc.wantTo)
			}
		})
	}
}

// =============================================================================
// writeRedirectConfig (HTTP)
// =============================================================================

func TestWriteRedirectConfig_HTTP(t *testing.T) {
	cfg := setupRedirectTestEnv(t)

	input := &redirectInput{
		name:      "old-test",
		domain:    "old.test",
		to:        "https://new.test",
		permanent: true,
		wildcard:  false,
	}
	if err := writeRedirectConfig(cfg, input); err != nil {
		t.Fatalf("writeRedirectConfig: %v", err)
	}

	path := filepath.Join(cfg.TraefikConfDir(), constants.RedirectConfigPrefix+input.name+constants.ExtYAML)
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

	input := &redirectInput{
		name:      "old-test",
		domain:    "old.test",
		to:        "https://new.test",
		permanent: false, // 302
		wildcard:  true,
	}
	if err := writeRedirectConfig(cfg, input); err != nil {
		t.Fatalf("writeRedirectConfig: %v", err)
	}

	path := filepath.Join(cfg.TraefikConfDir(), constants.RedirectConfigPrefix+input.name+constants.ExtYAML)
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
// writeRedirectDNSConfig (DNS-only)
// =============================================================================

func TestWriteRedirectDNSConfig(t *testing.T) {
	cfg := setupRedirectTestEnv(t)

	input := &redirectInput{
		name:    "alias",
		domain:  "old.test",
		to:      "new.example.com",
		dnsOnly: true,
	}
	if err := writeRedirectDNSConfig(cfg, input); err != nil {
		t.Fatalf("writeRedirectDNSConfig: %v", err)
	}

	path := filepath.Join(cfg.TraefikConfDir(), constants.RedirectConfigPrefix+input.name+constants.ExtYAML)
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

	input := &redirectInput{
		name:      "old-test",
		domain:    "old.test",
		to:        "https://new.test",
		permanent: true,
	}
	if err := writeRedirectConfig(cfg, input); err != nil {
		t.Fatalf("writeRedirectConfig: %v", err)
	}
	info := readRedirectConfig(cfg, input.name)

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

	input := &redirectInput{
		name:    "alias",
		domain:  "old.test",
		to:      "new.example.com",
		dnsOnly: true,
	}
	if err := writeRedirectDNSConfig(cfg, input); err != nil {
		t.Fatalf("writeRedirectDNSConfig: %v", err)
	}
	info := readRedirectConfig(cfg, input.name)

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
// cmd-side writer (writeRedirectDNSConfig) and the traefik-side scanner
// (ScanRedirectAliases). Renaming `source` or `target` on one side without
// updating the other would silently break the dnsmasq regen pipeline; this
// test makes the regression loud.
func TestDNSContractAcrossPackages(t *testing.T) {
	cfg := setupRedirectTestEnv(t)

	input := &redirectInput{
		name:    "alias",
		domain:  "old.test",
		to:      "new.example.com",
		dnsOnly: true,
	}
	if err := writeRedirectDNSConfig(cfg, input); err != nil {
		t.Fatalf("writeRedirectDNSConfig: %v", err)
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
	if err := writeRedirectConfig(cfg, &redirectInput{name: "alpha", domain: "alpha.test", to: "https://x.test", permanent: true}); err != nil {
		t.Fatal(err)
	}
	if err := writeRedirectDNSConfig(cfg, &redirectInput{name: "beta", domain: "beta.test", to: "y.test", dnsOnly: true}); err != nil {
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
