package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
)

func TestReloadOneMissingSite(t *testing.T) {
	setupSrvRoot(t)
	if err := reloadOne("ghost"); err == nil {
		t.Error("expected err")
	}
}

func TestReloadOneStatic(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: projectDir,
		Port:        80,
		NetworkName: "n",
	})
	if err := reloadOne("blog"); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestReloadOneWithRestart(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: projectDir,
		Port:        80,
		NetworkName: "n",
	})

	// Capture compose calls so reloadOne's restart path succeeds without docker.
	called := false
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error {
		called = true
		return nil
	}))

	reloadFlags.restart = true
	defer func() { reloadFlags.restart = false }()

	if err := reloadOne("blog"); err != nil {
		t.Errorf("err: %v", err)
	}
	if !called {
		t.Error("expected compose up to fire")
	}
}

func TestRunReloadAllEmpty(t *testing.T) {
	setupSrvRoot(t)
	reloadFlags.all = true
	defer func() { reloadFlags.all = false }()
	if err := runReload(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunReloadSingle(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: projectDir,
		Port:        80,
		NetworkName: "n",
	})
	reloadFlags.all = false
	if err := runReload(nil, []string{"blog"}); err != nil {
		t.Errorf("err: %v", err)
	}
}
