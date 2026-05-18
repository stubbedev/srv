package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/site"
)

func TestTypeLabel(t *testing.T) {
	if TypeLabel(true) == TypeLabel(false) {
		t.Error("labels should differ")
	}
}

func TestCommandExists(t *testing.T) {
	if !CommandExists("sh") {
		t.Error("sh should exist")
	}
	if CommandExists("definitely-not-a-binary-12345") {
		t.Error("bogus binary should not exist")
	}
}

func TestRunCommandDetached(t *testing.T) {
	// "true" returns immediately with exit 0.
	if err := RunCommandDetached("true"); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestGetSiteAliasesMissing(t *testing.T) {
	setupSrvRoot(t)
	if got := GetSiteAliases("ghost"); got != nil {
		t.Errorf("got %v", got)
	}
}

func TestGetSiteAliasesNoAliases(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/tmp",
		Port:        80,
		NetworkName: "n",
	})
	if got := GetSiteAliases("blog"); got != nil {
		t.Errorf("got %v", got)
	}
}

func TestGetSiteAliasesMany(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local", "a.local", "b.local"},
		ProjectPath: "/tmp",
		Port:        80,
		NetworkName: "n",
	})
	got := GetSiteAliases("blog")
	if len(got) != 2 {
		t.Errorf("got %v", got)
	}
}

func TestGetSiteRouteIDsEmpty(t *testing.T) {
	setupSrvRoot(t)
	if got := GetSiteRouteIDs("ghost"); got != nil {
		t.Errorf("got %v", got)
	}
}

func TestGetSiteRouteIDsWithRoutes(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/tmp",
		Port:        80,
		NetworkName: "n",
		Routes: []site.Route{
			{ID: "r1", Path: "/x", Upstream: site.Upstream{Kind: "localhost", Port: 80}},
		},
	})
	got := GetSiteRouteIDs("blog")
	if len(got) != 1 || got[0] != "r1" {
		t.Errorf("got %v", got)
	}
}

func TestGetSiteFromArgsByName(t *testing.T) {
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
	s, err := GetSiteFromArgs([]string{"blog"})
	if err != nil {
		t.Fatal(err)
	}
	if s == nil || s.Name != "blog" {
		t.Errorf("got %+v", s)
	}
}

func TestGetSiteFromArgsNoArgsNoSite(t *testing.T) {
	setupSrvRoot(t)
	s, err := GetSiteFromArgs(nil)
	if err != nil {
		t.Errorf("err: %v", err)
	}
	if s != nil {
		t.Errorf("got %+v, want nil", s)
	}
}

func TestGetSiteFromArgsRequiredMissing(t *testing.T) {
	setupSrvRoot(t)
	if _, err := GetSiteFromArgsRequired(nil); err == nil {
		t.Error("expected err")
	}
}

func TestSetVersion(t *testing.T) {
	pv, pc, pd := Version, Commit, BuildDate
	defer func() { Version, Commit, BuildDate = pv, pc, pd }()
	SetVersion("v9.9.9", "abcdef", "today")
	if Version != "v9.9.9" || Commit != "abcdef" || BuildDate != "today" {
		t.Errorf("got %q %q %q", Version, Commit, BuildDate)
	}
}

func TestExecuteVersionCommand(t *testing.T) {
	// Invoke Execute with a benign subcommand. The "version" cmd just prints.
	prev := os.Args
	defer func() { os.Args = prev }()
	RootCmd.SetArgs([]string{"version"})
	defer RootCmd.SetArgs(nil)
	if err := Execute(); err != nil {
		t.Errorf("err: %v", err)
	}
}
