package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
)

func TestRunBatchSiteOperationEmpty(t *testing.T) {
	if err := runBatchSiteOperation(nil, "start", func(*site.Site) error { return nil }); err != nil {
		t.Errorf("nil sites -> %v", err)
	}
}

func TestRunBatchSiteOperationSkipsBroken(t *testing.T) {
	sites := []site.Site{
		{Name: "ok", ComposeDir: "/x"},
		{Name: "broken", IsBroken: true},
	}
	called := 0
	if err := runBatchSiteOperation(sites, "start", func(*site.Site) error {
		called++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Errorf("expected 1 call (broken skipped), got %d", called)
	}
}

func TestRunBatchSiteOperationCollectsFailures(t *testing.T) {
	sites := []site.Site{
		{Name: "a"},
		{Name: "b"},
		{Name: "c"},
	}
	err := runBatchSiteOperation(sites, "start", func(s *site.Site) error {
		if s.Name == "b" {
			return errors.New("fail")
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected err for failed sites")
	}
	if !strings.Contains(err.Error(), "b") {
		t.Errorf("err should name failing site: %v", err)
	}
}

func TestRunRemoveMissingSite(t *testing.T) {
	setupSrvRoot(t)
	if err := runRemove(nil, []string{"ghost"}); err == nil {
		t.Error("expected err")
	}
}

func TestRunRemoveCompleteFlow(t *testing.T) {
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
		IsLocal:     true,
		NetworkName: "n",
	})
	// Stub docker compose so removal doesn't error.
	if err := runRemove(nil, []string{"blog"}); err != nil {
		t.Errorf("err: %v", err)
	}
	// Metadata should be gone.
	if site.HasSiteMetadata("blog") {
		t.Error("metadata should be removed")
	}
}

func TestRunStartDockerDown(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientErr(errors.New("offline")))
	if err := runStart(nil, []string{"ghost"}); err == nil {
		t.Error("expected err: docker offline")
	}
}

func TestRunStartNotInitialized(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientOK())
	// NetworkExists returns false (noopSDK.NetworkList returns nil), so
	// EnsureInitialized fails.
	if err := runStart(nil, []string{"ghost"}); err == nil {
		t.Error("expected err: not initialized")
	}
}


func TestRunStopDockerDown(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientErr(errors.New("offline")))
	if err := runStop(nil, []string{"x"}); err == nil {
		t.Error("expected err")
	}
}

func TestRunRestartDockerDown(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientErr(errors.New("offline")))
	if err := runRestart(nil, []string{"x"}); err == nil {
		t.Error("expected err")
	}
}

func TestStartAllSitesEmpty(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientOK())
	if err := startAllSites(); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestStopAllSitesEmpty(t *testing.T) {
	setupSrvRoot(t)
	if err := stopAllSites(); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRestartAllSitesEmpty(t *testing.T) {
	setupSrvRoot(t)
	if err := restartAllSites(); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunStartHappy(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := mustLoadConfig(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: projectDir,
		Port:        80,
		IsLocal:     false,
		NetworkName: cfg.NetworkName,
	})
	t.Cleanup(docker.SwapNewClientWithNetwork(cfg.NetworkName))
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	if err := runStart(nil, []string{"blog"}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunStartAllHappy(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := mustLoadConfig(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: projectDir,
		Port:        80,
		IsLocal:     false,
		NetworkName: cfg.NetworkName,
	})
	t.Cleanup(docker.SwapNewClientWithNetwork(cfg.NetworkName))
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	startFlags.all = true
	defer func() { startFlags.all = false }()
	if err := runStart(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunStopHappy(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := mustLoadConfig(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: projectDir,
		Port:        80,
		NetworkName: cfg.NetworkName,
	})
	t.Cleanup(docker.SwapNewClientWithNetwork(cfg.NetworkName))
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	if err := runStop(nil, []string{"blog"}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunRestartHappy(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := mustLoadConfig(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: projectDir,
		Port:        80,
		NetworkName: cfg.NetworkName,
	})
	t.Cleanup(docker.SwapNewClientWithNetwork(cfg.NetworkName))
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	if err := runRestart(nil, []string{"blog"}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunStartBuild(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := mustLoadConfig(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: projectDir,
		Port:        80,
		NetworkName: cfg.NetworkName,
	})
	t.Cleanup(docker.SwapNewClientWithNetwork(cfg.NetworkName))
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	startFlags.build = true
	defer func() { startFlags.build = false }()
	if err := runStart(nil, []string{"blog"}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunStartBroken(t *testing.T) {
	setupSrvRoot(t)
	cfg := mustLoadConfig(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/nonexistent-srv-broken",
		Port:        80,
		NetworkName: cfg.NetworkName,
	})
	t.Cleanup(docker.SwapNewClientWithNetwork(cfg.NetworkName))
	if err := runStart(nil, []string{"blog"}); err == nil {
		t.Error("expected err: site broken")
	}
}

func TestRunStopBroken(t *testing.T) {
	setupSrvRoot(t)
	cfg := mustLoadConfig(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/nonexistent-srv-broken",
		Port:        80,
		NetworkName: cfg.NetworkName,
	})
	t.Cleanup(docker.SwapNewClientOK())
	if err := runStop(nil, []string{"blog"}); err == nil {
		t.Error("expected err: site broken")
	}
}

func TestRunRestartBroken(t *testing.T) {
	setupSrvRoot(t)
	cfg := mustLoadConfig(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/nonexistent-srv-broken",
		Port:        80,
		NetworkName: cfg.NetworkName,
	})
	t.Cleanup(docker.SwapNewClientWithNetwork(cfg.NetworkName))
	if err := runRestart(nil, []string{"blog"}); err == nil {
		t.Error("expected err: site broken")
	}
}

func TestRunRestartBuild(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := mustLoadConfig(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: projectDir,
		Port:        80,
		NetworkName: cfg.NetworkName,
	})
	t.Cleanup(docker.SwapNewClientWithNetwork(cfg.NetworkName))
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	restartFlags.build = true
	defer func() { restartFlags.build = false }()
	if err := runRestart(nil, []string{"blog"}); err != nil {
		t.Errorf("err: %v", err)
	}
}
