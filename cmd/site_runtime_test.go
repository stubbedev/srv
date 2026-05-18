package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
)

func TestRunRuntimeMissingSite(t *testing.T) {
	setupSrvRoot(t)
	if err := runRuntime(nil, []string{"ghost"}); err == nil {
		t.Error("expected err")
	}
}

func TestRunRuntimeWrongType(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "x", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"x.local"},
		ProjectPath: "/tmp",
		Port:        80,
		NetworkName: "n",
	})
	if err := runRuntime(nil, []string{"x"}); err == nil {
		t.Error("expected err for static site")
	}
}

func TestRunRuntimePHPNoFlags(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypePHP,
		Domains:     []string{"blog.local"},
		ProjectPath: projectDir,
		Port:        80,
		NetworkName: "n",
		PHPVersion:  "8.3",
	})
	resetRuntimeFlags()
	if err := runRuntime(nil, []string{"blog"}); err == nil {
		t.Error("expected err: no flags")
	}
}

func TestRunRuntimePHPVersionUpdate(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:          site.SiteTypePHP,
		Domains:       []string{"blog.local"},
		ProjectPath:   projectDir,
		Port:          80,
		NetworkName:   "n",
		PHPVersion:    "8.3",
		PHPExtensions: []string{"redis"},
	})
	resetRuntimeFlags()
	runtimeFlags.phpVersion = "8.4"
	defer resetRuntimeFlags()

	if err := runRuntime(nil, []string{"blog"}); err != nil {
		t.Fatalf("err: %v", err)
	}
	meta, _ := site.ReadSiteMetadata("blog")
	if meta.PHPVersion != "8.4" {
		t.Errorf("version not updated: %q", meta.PHPVersion)
	}
}

func TestRunRuntimeNodeNoFlag(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "app", site.SiteMetadata{
		Type:        site.SiteTypeNode,
		Domains:     []string{"app.local"},
		ProjectPath: "/tmp",
		Port:        3000,
		NetworkName: "n",
	})
	resetRuntimeFlags()
	if err := runRuntime(nil, []string{"app"}); err == nil {
		t.Error("expected err: missing --node-version")
	}
}

func TestRunRuntimeNodeWrongRuntime(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "app", site.SiteMetadata{
		Type:        site.SiteTypeNode,
		Domains:     []string{"app.local"},
		ProjectPath: "/tmp",
		Port:        3000,
		NetworkName: "n",
		NodeRuntime: "bun",
	})
	resetRuntimeFlags()
	runtimeFlags.nodeVersion = "22"
	defer resetRuntimeFlags()
	if err := runRuntime(nil, []string{"app"}); err == nil {
		t.Error("expected err for non-node runtime")
	}
}

func TestRunRuntimeNodeUpdate(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	writeTestSite(t, "app", site.SiteMetadata{
		Type:        site.SiteTypeNode,
		Domains:     []string{"app.local"},
		ProjectPath: projectDir,
		Port:        3000,
		NetworkName: "n",
		NodeVersion: "20",
	})
	resetRuntimeFlags()
	runtimeFlags.nodeVersion = "22"
	defer resetRuntimeFlags()

	if err := runRuntime(nil, []string{"app"}); err != nil {
		t.Fatalf("err: %v", err)
	}
	meta, _ := site.ReadSiteMetadata("app")
	if meta.NodeVersion != "22" {
		t.Errorf("version not updated: %q", meta.NodeVersion)
	}
}

func TestRunEditMissingSite(t *testing.T) {
	setupSrvRoot(t)
	if err := runEdit(nil, []string{"ghost"}); err == nil {
		t.Error("expected err")
	}
}

func TestRunEditNoEditor(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/tmp",
		Port:        80,
		NetworkName: "n",
	})
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	if err := runEdit(nil, []string{"blog"}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunRegenerateMissingSite(t *testing.T) {
	setupSrvRoot(t)
	if err := runRegenerate(nil, []string{"ghost"}); err == nil {
		t.Error("expected err")
	}
}

func resetRuntimeFlags() {
	runtimeFlags.phpVersion = ""
	runtimeFlags.phpExtensions = ""
	runtimeFlags.nodeVersion = ""
}
