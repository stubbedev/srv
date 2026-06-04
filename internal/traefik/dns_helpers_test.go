package traefik

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/config"
)

func TestIsUnderRoutingTLD(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"foo.test", true},
		{"a.b.test", true},
		{"foo.localhost", true},
		{"foo.com", false},
		{"test", true},
		{"localhost", true},
		{"", false},
		{"falsetest", false},
		// .local is NOT TLD-wide routed (reserved for mDNS) — per-name only.
		{"foo.local", false},
		{"local", false},
	}
	for _, c := range cases {
		if got := isUnderRoutingTLD(c.in); got != c.want {
			t.Errorf("isUnderRoutingTLD(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestBuildDnsmasqConfNoWildcards(t *testing.T) {
	out := buildDnsmasqConf(nil, nil, []string{"8.8.8.8"})
	if !strings.Contains(out, "No wildcard domains") {
		t.Error("missing no-wildcard comment")
	}
	if !strings.Contains(out, "server=8.8.8.8") {
		t.Error("upstream missing")
	}
	if !strings.Contains(out, "hostsdir=/etc/dnsmasq.hosts") {
		t.Error("hostsdir missing")
	}
}

func TestBuildDnsmasqConfWildcards(t *testing.T) {
	out := buildDnsmasqConf([]string{"foo.local", "bar.local"}, nil, []string{"1.1.1.1"})
	if !strings.Contains(out, "address=/foo.local/127.0.0.1") {
		t.Error("wildcard 1 missing")
	}
	if !strings.Contains(out, "address=/bar.local/127.0.0.1") {
		t.Error("wildcard 2 missing")
	}
}

func TestBuildDnsmasqConfAliasResolved(t *testing.T) {
	aliases := []ResolvedAlias{
		{DNSAlias: DNSAlias{Source: "old.local", Target: "new.local"}, IP: "10.0.0.1"},
	}
	out := buildDnsmasqConf(nil, aliases, []string{"8.8.8.8"})
	if !strings.Contains(out, "address=/old.local/10.0.0.1") {
		t.Error("alias address missing")
	}
}

func TestBuildDnsmasqConfAliasResolveErr(t *testing.T) {
	aliases := []ResolvedAlias{
		{DNSAlias: DNSAlias{Source: "old.local", Target: "missing"}, ResolveErr: errors.New("nx")},
	}
	out := buildDnsmasqConf(nil, aliases, []string{"8.8.8.8"})
	if !strings.Contains(out, "resolution failed") {
		t.Error("alias error comment missing")
	}
	if strings.Contains(out, "address=/old.local/") {
		t.Error("failed alias should not emit address record")
	}
}

func TestBuildDnsmasqHosts(t *testing.T) {
	out := buildDnsmasqHosts([]string{"foo.local", "bar.local"})
	if !strings.Contains(out, "127.0.0.1 foo.local") {
		t.Error("entry 1 missing")
	}
	if !strings.Contains(out, "127.0.0.1 bar.local") {
		t.Error("entry 2 missing")
	}
}

func TestBuildDnsmasqHostsEmpty(t *testing.T) {
	out := buildDnsmasqHosts(nil)
	if !strings.HasPrefix(out, "#") {
		t.Error("empty hosts file should still have header")
	}
	if strings.Contains(out, "127.0.0.1 ") {
		t.Error("no host entries expected")
	}
}

func TestFileContentDiffers(t *testing.T) {
	cfg := newTraefikCfg(t)
	path := cfg.Root + "/test.conf"
	if !fileContentDiffers(path, "x") {
		t.Error("missing file should differ")
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fileContentDiffers(path, "x") {
		t.Error("matching content should not differ")
	}
	if !fileContentDiffers(path, "y") {
		t.Error("different content should differ")
	}
}

func TestSaveLoadLocalDomains(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	config.ResetCache()
	t.Cleanup(config.ResetCache)
	if err := os.MkdirAll(root+"/traefik", 0o755); err != nil {
		t.Fatal(err)
	}

	if err := SaveLocalDomains([]string{"foo.local", "bar.local", "foo.local"}); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadLocalDomains()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Errorf("expected 2 unique domains, got %v", loaded)
	}
	if loaded[0] != "bar.local" || loaded[1] != "foo.local" {
		t.Errorf("expected sorted, got %v", loaded)
	}
}

func TestLoadLocalDomainsMissing(t *testing.T) {
	t.Setenv("SRV_ROOT", t.TempDir())
	config.ResetCache()
	t.Cleanup(config.ResetCache)
	domains, err := LoadLocalDomains()
	if err != nil {
		t.Fatal(err)
	}
	if len(domains) != 0 {
		t.Errorf("expected empty, got %v", domains)
	}
}
