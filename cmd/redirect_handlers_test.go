package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/mkcert"
)

func TestGetRedirectNamesEmpty(t *testing.T) {
	setupSrvRoot(t)
	if got := getRedirectNames(); len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestGetRedirectNamesFinds(t *testing.T) {
	setupSrvRoot(t)
	cfg, _ := config.Load()
	confDir := cfg.TraefikConfDir()
	if err := os.WriteFile(filepath.Join(confDir, "redirect-foo.yml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(confDir, "redirect-bar.yml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(confDir, "other.yml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	names := getRedirectNames()
	if len(names) != 2 {
		t.Errorf("expected 2, got %v", names)
	}
}

func TestRunRedirectListEmpty(t *testing.T) {
	setupSrvRoot(t)
	if err := runRedirectList(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunRedirectRemoveMissing(t *testing.T) {
	setupSrvRoot(t)
	if err := runRedirectRemove(nil, []string{"ghost"}); err == nil {
		t.Error("expected err")
	}
}

func TestValidateRedirectInputDNSOnly(t *testing.T) {
	redirectAddFlags.domain = "old.com"
	redirectAddFlags.to = "new.com"
	redirectAddFlags.dnsOnly = true
	redirectAddFlags.name = "alias"
	redirectAddFlags.wildcard = false
	redirectAddFlags.temporary = false
	defer resetRedirectFlags()

	in, err := validateRedirectInput()
	if err != nil {
		t.Fatal(err)
	}
	if !in.dnsOnly || in.to != "new.com" {
		t.Errorf("got %+v", in)
	}
}

func TestValidateRedirectInputDNSOnlyRejectsScheme(t *testing.T) {
	redirectAddFlags.domain = "old.com"
	redirectAddFlags.to = "https://new.com"
	redirectAddFlags.dnsOnly = true
	defer resetRedirectFlags()

	if _, err := validateRedirectInput(); err == nil {
		t.Error("expected err for scheme in DNS target")
	}
}

func TestValidateRedirectInputDNSOnlyRejectsWildcard(t *testing.T) {
	redirectAddFlags.domain = "old.com"
	redirectAddFlags.to = "new.com"
	redirectAddFlags.dnsOnly = true
	redirectAddFlags.wildcard = true
	defer resetRedirectFlags()

	if _, err := validateRedirectInput(); err == nil {
		t.Error("expected err: --wildcard not allowed with --dns-only")
	}
}

func TestValidateRedirectInputDNSOnlyRejectsTemporary(t *testing.T) {
	redirectAddFlags.domain = "old.com"
	redirectAddFlags.to = "new.com"
	redirectAddFlags.dnsOnly = true
	redirectAddFlags.temporary = true
	defer resetRedirectFlags()

	if _, err := validateRedirectInput(); err == nil {
		t.Error("expected err: --temporary not allowed with --dns-only")
	}
}

func TestValidateRedirectInputHTTP(t *testing.T) {
	redirectAddFlags.domain = "old.com"
	redirectAddFlags.to = "https://new.com/"
	defer resetRedirectFlags()
	in, err := validateRedirectInput()
	if err != nil {
		t.Fatal(err)
	}
	if in.to != "https://new.com" {
		t.Errorf("trailing slash not stripped: %q", in.to)
	}
	if !in.permanent {
		t.Error("default should be permanent (301)")
	}
}

func TestValidateRedirectInputBadDomain(t *testing.T) {
	redirectAddFlags.domain = "bad domain"
	redirectAddFlags.to = "https://x.com"
	defer resetRedirectFlags()
	if _, err := validateRedirectInput(); err == nil {
		t.Error("expected err")
	}
}

func TestValidateRedirectInputBadURL(t *testing.T) {
	redirectAddFlags.domain = "x.com"
	redirectAddFlags.to = "ftp://x.com"
	defer resetRedirectFlags()
	if _, err := validateRedirectInput(); err == nil {
		t.Error("expected err: bad scheme")
	}
}

func TestValidateRedirectInputDerivesName(t *testing.T) {
	redirectAddFlags.domain = "old.example.com"
	redirectAddFlags.to = "https://new.example.com"
	defer resetRedirectFlags()
	in, err := validateRedirectInput()
	if err != nil {
		t.Fatal(err)
	}
	if in.name == "" {
		t.Error("name should be auto-derived")
	}
}

func TestRunRedirectReloadEmpty(t *testing.T) {
	setupSrvRoot(t)
	if err := runRedirectReload(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunRedirectListWithRedirects(t *testing.T) {
	setupSrvRoot(t)
	cfg, _ := config.Load()
	// HTTP redirect.
	t.Cleanup(mkcertSwapForCmd())
	resetRedirectFlags()
	redirectAddFlags.domain = "old.com"
	redirectAddFlags.to = "https://new.com"
	redirectAddFlags.name = "alias"
	if err := runRedirectAdd(nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := runRedirectList(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
	_ = cfg
	resetRedirectFlags()
}

func TestRunRedirectReloadWithEntries(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(mkcertSwapForCmd())
	resetRedirectFlags()
	redirectAddFlags.domain = "old.com"
	redirectAddFlags.to = "https://new.com"
	redirectAddFlags.name = "alias"
	if err := runRedirectAdd(nil, nil); err != nil {
		t.Fatal(err)
	}
	resetRedirectFlags()
	if err := runRedirectReload(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunRedirectRemoveHTTP(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(mkcertSwapForCmd())
	resetRedirectFlags()
	redirectAddFlags.domain = "old.com"
	redirectAddFlags.to = "https://new.com"
	redirectAddFlags.name = "alias"
	if err := runRedirectAdd(nil, nil); err != nil {
		t.Fatal(err)
	}
	resetRedirectFlags()
	if err := runRedirectRemove(nil, []string{"alias"}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunRedirectRemoveDNSOnly(t *testing.T) {
	setupSrvRoot(t)
	resetRedirectFlags()
	redirectAddFlags.domain = "old.com"
	redirectAddFlags.to = "new.com"
	redirectAddFlags.dnsOnly = true
	redirectAddFlags.name = "alias"
	if err := runRedirectAdd(nil, nil); err != nil {
		t.Fatal(err)
	}
	resetRedirectFlags()
	if err := runRedirectRemove(nil, []string{"alias"}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func mkcertSwapForCmd() func() {
	return mkcert.SwapRunner(stubMkcertRunner{})
}

func resetRedirectFlags() {
	redirectAddFlags.domain = ""
	redirectAddFlags.to = ""
	redirectAddFlags.name = ""
	redirectAddFlags.dnsOnly = false
	redirectAddFlags.wildcard = false
	redirectAddFlags.temporary = false
	redirectAddFlags.force = false
}

func TestRunRedirectAddDNSOnlyHappy(t *testing.T) {
	setupSrvRoot(t)
	redirectAddFlags.domain = "old.com"
	redirectAddFlags.to = "new.com"
	redirectAddFlags.dnsOnly = true
	redirectAddFlags.name = "alias"
	defer resetRedirectFlags()
	if err := runRedirectAdd(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunRedirectAddBadInput(t *testing.T) {
	setupSrvRoot(t)
	redirectAddFlags.domain = "bad space"
	redirectAddFlags.to = "https://x.com"
	defer resetRedirectFlags()
	if err := runRedirectAdd(nil, nil); err == nil {
		t.Error("expected err")
	}
}
