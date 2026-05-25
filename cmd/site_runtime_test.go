package cmd

import (
	"testing"

	"github.com/stubbedev/srv/internal/site"
)

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
