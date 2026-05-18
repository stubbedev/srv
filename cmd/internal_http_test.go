package cmd

import (
	"os"
	"testing"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/site"
)

func TestRunInternalEnableMissingSite(t *testing.T) {
	setupSrvRoot(t)
	if err := runInternalEnable(nil, []string{"ghost"}); err == nil {
		t.Error("expected err")
	}
}

func TestRunInternalEnableSuccess(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/tmp/blog",
		Port:        80,
		NetworkName: "n",
	})
	if err := runInternalEnable(nil, []string{"blog"}); err != nil {
		t.Errorf("err: %v", err)
	}
	meta, _ := site.ReadSiteMetadata("blog")
	if !site.HasListener(meta.Listeners, constants.ListenerInternal) {
		t.Error("listener not set")
	}
}

func TestRunInternalEnableIdempotent(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/tmp/blog",
		Port:        80,
		NetworkName: "n",
		Listeners:   []string{constants.ListenerInternal},
	})
	if err := runInternalEnable(nil, []string{"blog"}); err != nil {
		t.Errorf("idempotent enable -> %v", err)
	}
}

func TestRunInternalDisableSuccess(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/tmp/blog",
		Port:        80,
		NetworkName: "n",
		Listeners:   []string{constants.ListenerInternal},
	})
	if err := runInternalDisable(nil, []string{"blog"}); err != nil {
		t.Errorf("err: %v", err)
	}
	meta, _ := site.ReadSiteMetadata("blog")
	if site.HasListener(meta.Listeners, constants.ListenerInternal) {
		t.Error("listener should be removed")
	}
}

func TestRunInternalDisableMissingListener(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/tmp/blog",
		Port:        80,
		NetworkName: "n",
	})
	if err := runInternalDisable(nil, []string{"blog"}); err != nil {
		t.Errorf("should be no-op when not enabled, got %v", err)
	}
}

func TestRunInternalDisableMissingSite(t *testing.T) {
	setupSrvRoot(t)
	if err := runInternalDisable(nil, []string{"ghost"}); err == nil {
		t.Error("expected err")
	}
}

func TestRunInternalListEmpty(t *testing.T) {
	setupSrvRoot(t)
	if err := runInternalList(nil, nil); err != nil {
		t.Errorf("empty list -> %v", err)
	}
}

func TestRunInternalListWithEnabled(t *testing.T) {
	root := setupSrvRoot(t)
	// Real project dir so List() includes it.
	projectDir := root + "/p"
	if err := mkAllDir(projectDir); err != nil {
		t.Fatal(err)
	}
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: projectDir,
		Port:        80,
		NetworkName: "n",
		Listeners:   []string{constants.ListenerInternal},
	})
	if err := runInternalList(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func mkAllDir(p string) error {
	return os.MkdirAll(p, 0o755)
}
