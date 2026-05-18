package site

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListEmpty(t *testing.T) {
	withSRVRoot(t)
	sites, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 0 {
		t.Errorf("expected 0, got %v", sites)
	}
}

func TestListSkipsUnderscoreDirs(t *testing.T) {
	root := withSRVRoot(t)
	// Internal dirs prefixed with _ should not be enumerated.
	if err := os.MkdirAll(filepath.Join(root, "sites", "_proxy"), 0o755); err != nil {
		t.Fatal(err)
	}
	sites, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 0 {
		t.Errorf("expected internal dir skipped, got %v", sites)
	}
}

func TestListBrokenSite(t *testing.T) {
	withSRVRoot(t)
	// Write metadata pointing at a missing project path.
	meta := SiteMetadata{
		Type:        SiteTypeStatic,
		Domains:     []string{"x.local"},
		ProjectPath: "/nonexistent/srv-test-broken",
		Port:        80,
		IsLocal:     true,
		NetworkName: "n",
	}
	if err := WriteSiteMetadata("broken", meta); err != nil {
		t.Fatal(err)
	}
	sites, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 {
		t.Fatalf("expected 1 site, got %d", len(sites))
	}
	if !sites[0].IsBroken {
		t.Error("expected IsBroken=true")
	}
}

func TestListValidSite(t *testing.T) {
	root := withSRVRoot(t)
	projectDir := filepath.Join(root, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := SiteMetadata{
		Type:        SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: projectDir,
		Port:        80,
		IsLocal:     true,
		NetworkName: "n",
		ServiceName: "blog-web",
	}
	if err := WriteSiteMetadata("blog", meta); err != nil {
		t.Fatal(err)
	}
	sites, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 {
		t.Fatalf("expected 1 site, got %d", len(sites))
	}
	s := sites[0]
	if s.Name != "blog" || s.Dir != projectDir || s.Domains[0] != "blog.local" {
		t.Errorf("got %+v", s)
	}
	if s.IsBroken {
		t.Error("should not be broken")
	}
}

func TestGetMissing(t *testing.T) {
	withSRVRoot(t)
	if _, err := Get("ghost"); err == nil {
		t.Error("expected err")
	}
}

func TestGetByNameMissing(t *testing.T) {
	withSRVRoot(t)
	if _, err := GetByName("ghost"); err == nil {
		t.Error("expected err")
	}
}

func TestGetByNameFound(t *testing.T) {
	root := withSRVRoot(t)
	projectDir := filepath.Join(root, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := SiteMetadata{
		Type:        SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: projectDir,
		Port:        80,
		IsLocal:     true,
		NetworkName: "n",
	}
	if err := WriteSiteMetadata("blog", meta); err != nil {
		t.Fatal(err)
	}
	got, err := GetByName("blog")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "blog" {
		t.Errorf("got %+v", got)
	}
}

func TestGetFound(t *testing.T) {
	root := withSRVRoot(t)
	projectDir := filepath.Join(root, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := SiteMetadata{
		Type:        SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: projectDir,
		Port:        80,
		IsLocal:     true,
		NetworkName: "n",
	}
	if err := WriteSiteMetadata("blog", meta); err != nil {
		t.Fatal(err)
	}
	got, err := Get("blog")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "blog" {
		t.Errorf("got %+v", got)
	}
}

func TestResolvePathExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got, err := ResolvePath("~/projects/x")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(home, "projects", "x") {
		t.Errorf("got %q", got)
	}
}

func TestResolvePathAbsolute(t *testing.T) {
	got, err := ResolvePath("/abs/path")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/abs/path" {
		t.Errorf("got %q", got)
	}
}

func TestExists(t *testing.T) {
	root := withSRVRoot(t)
	if Exists("nope") {
		t.Error("expected false")
	}
	siteDir := filepath.Join(root, "sites", "blog")
	if err := os.MkdirAll(siteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Without metadata.yml HasSiteMetadata returns false.
	if Exists("blog") {
		t.Error("expected false without metadata.yml")
	}
	meta := SiteMetadata{Type: SiteTypeStatic, Domains: []string{"x.local"}, ProjectPath: "/tmp/x", Port: 80, NetworkName: "n"}
	if err := WriteSiteMetadata("blog", meta); err != nil {
		t.Fatal(err)
	}
	if !Exists("blog") {
		t.Error("expected true after metadata write")
	}
}

func TestGenerateStaticContainerName(t *testing.T) {
	a := generateStaticContainerName("blog")
	b := generateStaticContainerName("blog")
	if a != b {
		t.Error("not deterministic")
	}
	c := generateStaticContainerName("other")
	if a == c {
		t.Error("different names should hash differently")
	}
}
