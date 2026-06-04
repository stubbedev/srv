package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/mkcert"
	"github.com/stubbedev/srv/internal/shell"
	"github.com/stubbedev/srv/internal/shell/shelltest"
	"github.com/stubbedev/srv/internal/site"
)

func setupSrvRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	config.ResetCache()
	t.Cleanup(config.ResetCache)
	// Fake shell so any handler path that registers a local domain (and thus
	// hits SetupDNS → sudo) cannot escape into the real system during tests.
	t.Cleanup(shell.SwapDefault(shelltest.New(nil)))
	// Fake mkcert so certificate-generating handlers don't invoke the real
	// `mkcert -install` (which prompts for sudo on NixOS) or pollute the
	// developer's user-scoped CAROOT directory.
	t.Cleanup(mkcert.SwapRunner(stubMkcertRunner{}))
	if err := os.MkdirAll(filepath.Join(root, "traefik", "conf"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeTestSite(t *testing.T, name string, meta site.SiteMetadata) {
	t.Helper()
	if err := site.WriteSiteMetadata(name, meta); err != nil {
		t.Fatal(err)
	}
}

func TestRunAliasListMissingSite(t *testing.T) {
	setupSrvRoot(t)
	if err := runAliasList(nil, []string{"ghost"}); err == nil {
		t.Error("expected err for missing site")
	}
}

func TestRunAliasListEmpty(t *testing.T) {
	setupSrvRoot(t)
	// Write a metadata file with empty domains list — should report "no
	// domains" and return nil.
	root, _ := config.Load()
	siteDir := filepath.Join(root.SitesDir, "x")
	if err := os.MkdirAll(siteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(siteDir, "metadata.yml"), []byte("type: static\nproject_path: /tmp/x\nport: 80\nnetwork_name: n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runAliasList(nil, []string{"x"}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunAliasListWithAliases(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local", "alt1.local", "alt2.local"},
		ProjectPath: "/tmp/blog",
		Port:        80,
		NetworkName: "n",
	})
	if err := runAliasList(nil, []string{"blog"}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunAliasListNoAliases(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/tmp/blog",
		Port:        80,
		NetworkName: "n",
	})
	if err := runAliasList(nil, []string{"blog"}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunAliasAddInvalidAlias(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/tmp/blog",
		Port:        80,
		NetworkName: "n",
	})
	if err := runAliasAdd(nil, []string{"blog", "BAD ALIAS WITH SPACES"}); err == nil {
		t.Error("expected err for invalid alias")
	}
}

func TestRunAliasAddMissingSite(t *testing.T) {
	setupSrvRoot(t)
	if err := runAliasAdd(nil, []string{"ghost", "new.local"}); err == nil {
		t.Error("expected err for missing site")
	}
}

func TestRunAliasAddDuplicate(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local", "alt.local"},
		ProjectPath: "/tmp/blog",
		Port:        80,
		NetworkName: "n",
	})
	if err := runAliasAdd(nil, []string{"blog", "alt.local"}); err != nil {
		t.Errorf("duplicate should be no-op, got %v", err)
	}
}

func TestRunAliasRemoveCanonicalErrors(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local", "alt.local"},
		ProjectPath: "/tmp/blog",
		Port:        80,
		NetworkName: "n",
	})
	if err := runAliasRemove(nil, []string{"blog", "blog.local"}); err == nil {
		t.Error("expected err removing canonical")
	}
}

func TestRunAliasRemoveMissing(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/tmp/blog",
		Port:        80,
		NetworkName: "n",
	})
	if err := runAliasRemove(nil, []string{"blog", "never.local"}); err == nil {
		t.Error("expected err for unregistered alias")
	}
}

func TestRunAliasRemoveMissingSite(t *testing.T) {
	setupSrvRoot(t)
	if err := runAliasRemove(nil, []string{"ghost", "x.local"}); err == nil {
		t.Error("expected err for missing site")
	}
}

// Routing regeneration moved to internal/site (regenerateRouting), covered by
// the alias/internal mutator tests there.

func TestRunAliasAddHappy(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/tmp",
		Port:        80,
		IsLocal:     true,
		NetworkName: "n",
	})
	if err := runAliasAdd(nil, []string{"blog", "alt.local"}); err != nil {
		t.Errorf("err: %v", err)
	}
	meta, _ := site.ReadSiteMetadata("blog")
	if len(meta.Domains) != 2 || meta.Domains[1] != "alt.local" {
		t.Errorf("got %v", meta.Domains)
	}
}

func TestRunAliasRemoveHappy(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local", "alt.local"},
		ProjectPath: "/tmp",
		Port:        80,
		IsLocal:     true,
		NetworkName: "n",
	})
	if err := runAliasRemove(nil, []string{"blog", "alt.local"}); err != nil {
		t.Errorf("err: %v", err)
	}
	meta, _ := site.ReadSiteMetadata("blog")
	if len(meta.Domains) != 1 {
		t.Errorf("got %v", meta.Domains)
	}
}
