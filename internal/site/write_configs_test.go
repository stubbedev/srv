package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/constants"
)

func TestWriteStaticSiteConfigCreatesFiles(t *testing.T) {
	root := withSRVRoot(t)
	meta := SiteMetadata{
		Type:        SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/srv/blog",
		Port:        80,
		IsLocal:     true,
		NetworkName: "tnet",
		SPA:         true,
		Cache:       true,
	}
	if _, err := WriteStaticSiteConfig("blog", meta, true); err != nil {
		t.Fatalf("WriteStaticSiteConfig err: %v", err)
	}
	siteDir := filepath.Join(root, "sites", "blog")
	for _, f := range []string{"nginx.conf", "docker-compose.yml"} {
		if _, err := os.Stat(filepath.Join(siteDir, f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	nginx, _ := os.ReadFile(filepath.Join(siteDir, "nginx.conf"))
	if !strings.Contains(string(nginx), "try_files $uri $uri/ /index.html") {
		t.Error("nginx.conf missing SPA fallback")
	}
}

func TestWriteStaticSiteConfigForceFalsePreserves(t *testing.T) {
	root := withSRVRoot(t)
	meta := SiteMetadata{
		Type:        SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/srv/blog",
		Port:        80,
		IsLocal:     true,
		NetworkName: "tnet",
	}
	if _, err := WriteStaticSiteConfig("blog", meta, true); err != nil {
		t.Fatal(err)
	}
	siteDir := filepath.Join(root, "sites", "blog")
	nginxPath := filepath.Join(siteDir, "nginx.conf")
	if err := os.WriteFile(nginxPath, []byte("MANUAL"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteStaticSiteConfig("blog", meta, false); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(nginxPath)
	if string(got) != "MANUAL" {
		t.Errorf("force=false should preserve, got %q", string(got))
	}
}

// Extra volumes and networks from metadata must be rendered into srv-managed
// compose files — writable from three surfaces but previously rendered by
// nothing, so the UI's "takes effect on next restart" promise was false.
func TestWriteStaticSiteConfigRendersVolumesAndNetworks(t *testing.T) {
	root := withSRVRoot(t)
	meta := SiteMetadata{
		Type:          SiteTypeStatic,
		Domains:       []string{"blog.local"},
		ProjectPath:   "/srv/blog",
		Port:          80,
		IsLocal:       true,
		NetworkName:   "tnet",
		ExtraNetworks: []string{"mysql01"},
		Volumes:       []VolumeMount{{Source: "/nix", Target: "/nix", ReadOnly: true}},
	}
	if _, err := WriteStaticSiteConfig("blog", meta, true); err != nil {
		t.Fatal(err)
	}
	compose, err := os.ReadFile(filepath.Join(root, "sites", "blog", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	// Long-syntax compose volumes render as source/target keys, not :ro shorthand.
	if !strings.Contains(string(compose), "source: /nix") {
		t.Errorf("extra volume not rendered:\n%s", compose)
	}
	if !strings.Contains(string(compose), "mysql01") {
		t.Errorf("extra network not rendered:\n%s", compose)
	}
}

func TestWriteDockerfileSiteConfig(t *testing.T) {
	root := withSRVRoot(t)
	meta := SiteMetadata{
		Type:        SiteTypeDockerfile,
		Domains:     []string{"app.local"},
		ProjectPath: "/srv/app",
		Port:        8080,
		IsLocal:     true,
		NetworkName: "tnet",
	}
	info := &DockerfileSiteInfo{Port: 8080}
	if err := WriteDockerfileSiteConfig("app", meta, info, true); err != nil {
		t.Fatalf("err: %v", err)
	}
	compose, err := os.ReadFile(filepath.Join(root, "sites", "app", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(compose), "build:") {
		t.Error("compose missing build directive")
	}
	if !strings.Contains(string(compose), constants.DockerfileFile) {
		t.Error("compose missing Dockerfile reference")
	}
}

// The daemon reloads with force=true, so a hand-edited generated file (both
// carry a "yours to edit" header) must never be silently reverted: the fresh
// render lands beside it as *.regenerated and the result carries a warning.
func TestForceRegenPreservesUserEditedFile(t *testing.T) {
	root := withSRVRoot(t)
	meta := SiteMetadata{
		Type:        SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/srv/blog",
		Port:        80,
		IsLocal:     true,
		NetworkName: "tnet",
	}
	if _, err := WriteStaticSiteConfig("blog", meta, true); err != nil {
		t.Fatal(err)
	}
	siteDir := filepath.Join(root, "sites", "blog")
	composePath := filepath.Join(siteDir, "docker-compose.yml")
	orig, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatal(err)
	}
	edited := string(orig) + "# my custom volume\n"
	if err := os.WriteFile(composePath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	warnings, err := WriteStaticSiteConfig("blog", meta, true)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != edited {
		t.Error("force regen reverted a user-edited compose file")
	}
	regen, err := os.ReadFile(composePath + ".regenerated")
	if err != nil {
		t.Fatalf("expected a .regenerated side file: %v", err)
	}
	if string(regen) != string(orig) {
		t.Error(".regenerated does not carry the fresh render")
	}
	if len(warnings) == 0 {
		t.Error("expected a merge warning for the preserved file")
	}
}

// An untouched generated file keeps updating in place, and adopting the
// .regenerated content (copying it over the original) hands ownership back.
func TestForceRegenReclaimsAdoptedFile(t *testing.T) {
	root := withSRVRoot(t)
	meta := SiteMetadata{
		Type:        SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/srv/blog",
		Port:        80,
		IsLocal:     true,
		NetworkName: "tnet",
	}
	if _, err := WriteStaticSiteConfig("blog", meta, true); err != nil {
		t.Fatal(err)
	}
	siteDir := filepath.Join(root, "sites", "blog")
	composePath := filepath.Join(siteDir, "docker-compose.yml")

	// Untouched file: regen with a metadata change updates in place.
	meta.Cache = true
	warnings, err := WriteStaticSiteConfig("blog", meta, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("owned file should regen in place, got warnings %v", warnings)
	}
	if _, err := os.Stat(composePath + ".regenerated"); !os.IsNotExist(err) {
		t.Error("unexpected .regenerated for owned file")
	}

	// User edits, regen preserves, user adopts by copying back: next regen
	// owns the file again.
	orig, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(composePath, append(orig, []byte("# mine\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteStaticSiteConfig("blog", meta, true); err != nil {
		t.Fatal(err)
	}
	regen, err := os.ReadFile(composePath + ".regenerated")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(composePath, regen, 0o644); err != nil {
		t.Fatal(err)
	}
	meta.SPA = true
	warnings, err = WriteStaticSiteConfig("blog", meta, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("adopted file should regen in place, got warnings %v", warnings)
	}
	nginx, err := os.ReadFile(filepath.Join(siteDir, "nginx.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(nginx), "try_files") {
		t.Error("SPA change did not reach nginx.conf on the adopted site")
	}
	got, _ := os.ReadFile(composePath)
	if strings.Contains(string(got), "# mine\n") {
		t.Error("adopted compose file still carries the user's edit")
	}
}
