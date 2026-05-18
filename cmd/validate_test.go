package cmd

import (
	"testing"

	"github.com/stubbedev/srv/internal/site"
)

func TestValidateOneMissingSite(t *testing.T) {
	setupSrvRoot(t)
	if err := validateOne("ghost"); err == nil {
		t.Error("expected err")
	}
}

func TestValidateOneOK(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/tmp",
		Port:        80,
		NetworkName: "n",
	})
	if err := validateOne("blog"); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestValidateOneInvalid(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "bad", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     nil, // missing domain — invalid
		ProjectPath: "/tmp",
		Port:        80,
		NetworkName: "n",
	})
	if err := validateOne("bad"); err == nil {
		t.Error("expected err")
	}
}

func TestRunValidateAllEmpty(t *testing.T) {
	setupSrvRoot(t)
	validateFlags.all = true
	defer func() { validateFlags.all = false }()
	if err := runValidate(nil, nil); err != nil {
		t.Errorf("empty all -> %v", err)
	}
}

func TestRunValidateSingle(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/tmp",
		Port:        80,
		NetworkName: "n",
	})
	if err := runValidate(nil, []string{"blog"}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunValidateFailures(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "bad", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		ProjectPath: "/tmp",
		Port:        80,
		NetworkName: "n",
	})
	if err := runValidate(nil, []string{"bad"}); err == nil {
		t.Error("expected err")
	}
}
