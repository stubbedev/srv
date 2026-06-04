package traefik

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/config"
)

// writeRedirectFile is a small helper for table-driven tests that need to
// stage redirect-<name>.yml files in the traefik conf dir.
func writeRedirectFile(t *testing.T, name, body string) {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	confDir := cfg.TraefikConfDir()
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatalf("mkdir confdir: %v", err)
	}
	path := filepath.Join(confDir, "redirect-"+name+".yml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestScanRedirectAliases(t *testing.T) {
	defer setupDNSTestEnv(t)()

	t.Run("empty conf dir returns no aliases", func(t *testing.T) {
		aliases, err := ScanRedirectAliases()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(aliases) != 0 {
			t.Errorf("expected zero aliases, got %v", aliases)
		}
	})

	t.Run("picks up dns: block, ignores http: block", func(t *testing.T) {
		defer setupDNSTestEnv(t)()
		writeRedirectFile(t, "alias-one", `dns:
  source: old.test
  target: new.example.com
`)
		writeRedirectFile(t, "alias-two", `dns:
  source: another.test
  target: target.example.com
`)
		// An HTTP-style redirect must be excluded from the scan — it is
		// handled by Traefik's file provider, not dnsmasq.
		writeRedirectFile(t, "http-style", `http:
  routers:
    redirect-foo:
      rule: Host(`+"`foo.test`"+`)
      service: noop
`)
		// Files without the redirect- prefix must be ignored.
		cfg, _ := config.Load()
		stray := filepath.Join(cfg.TraefikConfDir(), "site-foo.yml")
		if err := os.WriteFile(stray, []byte("dns:\n  source: bad.test\n  target: x.test\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		aliases, err := ScanRedirectAliases()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(aliases) != 2 {
			t.Fatalf("expected 2 aliases, got %d (%+v)", len(aliases), aliases)
		}
		seen := map[string]string{}
		for _, a := range aliases {
			seen[a.Source] = a.Target
		}
		if seen["old.test"] != "new.example.com" {
			t.Errorf("missing old.test alias: %v", seen)
		}
		if seen["another.test"] != "target.example.com" {
			t.Errorf("missing another.test alias: %v", seen)
		}
		if _, found := seen["bad.test"]; found {
			t.Errorf("non-redirect file leaked into scan: %v", seen)
		}
	})

	t.Run("malformed yaml is skipped, not fatal", func(t *testing.T) {
		defer setupDNSTestEnv(t)()
		writeRedirectFile(t, "broken", "dns: this is not a map :::")
		writeRedirectFile(t, "good", `dns:
  source: good.test
  target: ok.test
`)
		aliases, err := ScanRedirectAliases()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(aliases) != 1 || aliases[0].Source != "good.test" {
			t.Errorf("malformed file should be skipped; got %+v", aliases)
		}
	})

	// These redirect-*.yml files are hand-editable, so a crafted source/target
	// could otherwise inject extra dnsmasq directives via the `address=/…/…`
	// line. ScanRedirectAliases must re-validate and drop bad entries.
	t.Run("rejects source/target that could inject dnsmasq directives", func(t *testing.T) {
		injections := map[string]string{
			// A slash closes the address= token and starts a new directive.
			"inject-source": "dns:\n  source: \"evil.test/127.0.0.1\\nlog-queries\"\n  target: ok.test\n",
			// Whitespace / newline in the target.
			"inject-target": "dns:\n  source: ok.test\n  target: \"x.test\\nserver=/evil/6.6.6.6\"\n",
			// A bare slash in the source.
			"slash-source": "dns:\n  source: \"a/b.test\"\n  target: ok.test\n",
			// Empty source.
			"empty-source": "dns:\n  source: \"\"\n  target: ok.test\n",
		}
		t.Run("negative: malicious entries are skipped", func(t *testing.T) {
			defer setupDNSTestEnv(t)()
			for name, body := range injections {
				writeRedirectFile(t, name, body)
			}
			aliases, err := ScanRedirectAliases()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(aliases) != 0 {
				t.Errorf("expected all malicious entries skipped, got %+v", aliases)
			}
		})

		t.Run("positive: a clean entry alongside bad ones still parses", func(t *testing.T) {
			defer setupDNSTestEnv(t)()
			for name, body := range injections {
				writeRedirectFile(t, name, body)
			}
			writeRedirectFile(t, "clean", "dns:\n  source: good.test\n  target: ok.example.com\n")
			aliases, err := ScanRedirectAliases()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(aliases) != 1 || aliases[0].Source != "good.test" || aliases[0].Target != "ok.example.com" {
				t.Errorf("only the clean entry should survive; got %+v", aliases)
			}
		})
	})
}

func TestResolveAliases(t *testing.T) {
	t.Run("empty input returns empty slice", func(t *testing.T) {
		out := ResolveAliases(nil)
		if len(out) != 0 {
			t.Errorf("expected empty slice, got %v", out)
		}
	})

	t.Run("unresolvable target produces ResolveErr", func(t *testing.T) {
		// `.invalid` is reserved by RFC 6761 to always fail DNS resolution,
		// so this case is hermetic across networks.
		aliases := []DNSAlias{{Source: "x.test", Target: "definitely-not-a-host.invalid"}}
		out := ResolveAliases(aliases)
		if len(out) != 1 {
			t.Fatalf("expected 1 result, got %d", len(out))
		}
		if out[0].ResolveErr == nil {
			t.Errorf("expected ResolveErr for .invalid target, got IP=%q", out[0].IP)
		}
		if out[0].IP != "" {
			t.Errorf("expected empty IP on failure, got %q", out[0].IP)
		}
		// Source/Target must round-trip unchanged so callers can map results.
		if out[0].Source != "x.test" || out[0].Target != "definitely-not-a-host.invalid" {
			t.Errorf("source/target lost in resolve: %+v", out[0])
		}
	})
}

func TestBuildDnsmasqConfWithAliases(t *testing.T) {
	t.Run("emits address= directive per resolved alias", func(t *testing.T) {
		aliases := []ResolvedAlias{
			{DNSAlias: DNSAlias{Source: "a.test", Target: "x.example.com"}, IP: "1.2.3.4"},
			{DNSAlias: DNSAlias{Source: "b.test", Target: "y.example.com"}, IP: "5.6.7.8"},
		}
		out := buildDnsmasqConf(nil, aliases, []string{"8.8.8.8"})
		for _, want := range []string{
			"address=/a.test/1.2.3.4",
			"address=/b.test/5.6.7.8",
			"# a.test -> x.example.com",
			"# DNS-alias redirects",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q in conf:\n%s", want, out)
			}
		}
	})

	t.Run("failed resolution becomes a commented-out skip line", func(t *testing.T) {
		aliases := []ResolvedAlias{
			{DNSAlias: DNSAlias{Source: "bad.test", Target: "broken.invalid"}, ResolveErr: errStub("nope")},
		}
		out := buildDnsmasqConf(nil, aliases, []string{"8.8.8.8"})
		if strings.Contains(out, "address=/bad.test/") {
			t.Errorf("failed alias must not emit address= directive:\n%s", out)
		}
		if !strings.Contains(out, "resolution failed") {
			t.Errorf("expected skip comment, got:\n%s", out)
		}
	})

	t.Run("nil aliases keeps the conf clean of the section", func(t *testing.T) {
		out := buildDnsmasqConf(nil, nil, []string{"8.8.8.8"})
		if strings.Contains(out, "DNS-alias redirects") {
			t.Errorf("nil aliases should not emit the alias section header:\n%s", out)
		}
	})
}

// errStub is a tiny error type for tests that need a non-nil error without
// pulling in errors.New everywhere.
type errStub string

func (e errStub) Error() string { return string(e) }
