package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/site"
)

func TestAllDomains(t *testing.T) {
	s := &siteSetup{domain: "blog.local", aliases: []string{"alt.local", "extra.local"}}
	got := s.allDomains()
	if len(got) != 3 || got[0] != "blog.local" {
		t.Errorf("got %v", got)
	}
}

func TestAllDomainsNoCanonical(t *testing.T) {
	s := &siteSetup{aliases: []string{"alt.local"}}
	got := s.allDomains()
	if len(got) != 1 || got[0] != "alt.local" {
		t.Errorf("got %v", got)
	}
}

func TestSetupSiteFilesStatic(t *testing.T) {
	root := setupSrvRoot(t)
	cfg, _ := config.Load()
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	setup := &siteSetup{
		siteName: "blog",
		sitePath: projectDir,
		domain:   "blog.local",
		port:     80,
		isLocal:  true,
		isStatic: true,
	}
	if err := setupSiteFiles(cfg, setup); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !site.HasSiteMetadata("blog") {
		t.Error("metadata should be written")
	}
}

func TestSetupSiteFilesNode(t *testing.T) {
	root := setupSrvRoot(t)
	cfg, _ := config.Load()
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	setup := &siteSetup{
		siteName: "app",
		sitePath: projectDir,
		domain:   "app.local",
		port:     3000,
		isLocal:  true,
		isNode:   true,
		nodeInfo: site.NodeDefaults(),
	}
	if err := setupSiteFiles(cfg, setup); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestSetupSiteFilesCompose(t *testing.T) {
	root := setupSrvRoot(t)
	cfg, _ := config.Load()
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	setup := &siteSetup{
		siteName:    "compose",
		sitePath:    projectDir,
		domain:      "app.local",
		port:        8080,
		isLocal:     true,
		serviceName: "compose-web",
		composePath: filepath.Join(projectDir, "docker-compose.yml"),
	}
	if err := setupSiteFiles(cfg, setup); err != nil {
		t.Fatalf("err: %v", err)
	}
}
