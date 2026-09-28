package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/traefik"
)

func TestResolvePortConflictsManual(t *testing.T) {
	// Unknown process → manual error.
	conflicts := []traefik.PortConflict{
		{Port: 80, Name: "HTTP", Process: ""},
	}
	if err := resolvePortConflicts(conflicts); err == nil {
		t.Error("expected err: manual conflict")
	}
}

func TestResolvePortConflictsManualNamedUnfixable(t *testing.T) {
	conflicts := []traefik.PortConflict{
		{Port: 80, Name: "HTTP", Process: "totally-foreign-process"},
	}
	if err := resolvePortConflicts(conflicts); err == nil {
		t.Error("expected err: non-autofix process")
	}
}

func TestStartSitesEmpty(t *testing.T) {
	// startSites with no sites should be a noop.
	startSites(nil)
}

// Regression for #12: install's site step must route a daemon-served site
// (no compose file, no compose dir) instead of running compose on it.
func TestStartSitesDaemonServedSkipsCompose(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestSite(t, "start-local", site.SiteMetadata{
		Type:         site.SiteTypeStatic,
		Domains:      []string{"start.local"},
		ProjectPath:  projectDir,
		DaemonServed: true,
	})
	t.Cleanup(docker.SwapComposeExec(func(dir string, _ bool, args ...string) error {
		t.Errorf("compose %v ran in %q for a daemon-served site", args, dir)
		return nil
	}))
	sites, err := site.ListBasic()
	if err != nil {
		t.Fatal(err)
	}
	startSites(sites)
	cfg := mustLoadConfig(t)
	if _, err := os.Stat(filepath.Join(cfg.TraefikConfDir(), "site-start-local.yml")); err != nil {
		t.Errorf("daemon-served site not routed: %v", err)
	}
}
