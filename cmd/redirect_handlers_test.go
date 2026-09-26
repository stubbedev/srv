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
	return mkcert.SwapEngine(&stubMkcertEngine{})
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
