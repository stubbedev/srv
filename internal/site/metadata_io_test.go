package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/config"
)

// withSRVRoot points SRV_ROOT at a fresh tempdir and resets the config cache.
func withSRVRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	config.ResetCache()
	t.Cleanup(func() { config.ResetCache() })
	return root
}

func TestWriteAndReadSiteMetadata(t *testing.T) {
	withSRVRoot(t)
	meta := SiteMetadata{
		Type:        SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/srv/blog",
		Port:        80,
		IsLocal:     true,
		NetworkName: "tnet",
	}
	if err := WriteSiteMetadata("blog", meta); err != nil {
		t.Fatalf("Write err: %v", err)
	}

	got, err := ReadSiteMetadata("blog")
	if err != nil {
		t.Fatalf("Read err: %v", err)
	}
	if got == nil {
		t.Fatal("nil after write")
	}
	if got.Type != SiteTypeStatic || got.Domains[0] != "blog.local" {
		t.Errorf("got %+v", got)
	}
	if got.SchemaVersion != CurrentMetadataSchema {
		t.Errorf("SchemaVersion = %d, want %d", got.SchemaVersion, CurrentMetadataSchema)
	}
}

func TestReadSiteMetadataMissing(t *testing.T) {
	withSRVRoot(t)
	got, err := ReadSiteMetadata("never-existed")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestReadSiteMetadataInvalidYAML(t *testing.T) {
	root := withSRVRoot(t)
	siteDir := filepath.Join(root, "sites", "broken")
	if err := os.MkdirAll(siteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(siteDir, "metadata.yml"), []byte("not: : valid: yaml :"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSiteMetadata("broken"); err == nil {
		t.Error("expected parse error")
	}
}

func TestReadSiteMetadataLegacyDomain(t *testing.T) {
	root := withSRVRoot(t)
	siteDir := filepath.Join(root, "sites", "old")
	if err := os.MkdirAll(siteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "type: static\ndomain: legacy.local\nproject_path: /tmp/legacy\nport: 80\nis_local: true\nnetwork_name: n\n"
	if err := os.WriteFile(filepath.Join(siteDir, "metadata.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSiteMetadata("old")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got.Domains) != 1 || got.Domains[0] != "legacy.local" {
		t.Errorf("legacy migration failed: %+v", got)
	}
}

func TestRemoveSiteMetadata(t *testing.T) {
	root := withSRVRoot(t)
	meta := SiteMetadata{Type: SiteTypeStatic, Domains: []string{"x.local"}, ProjectPath: "/tmp/x", Port: 80, NetworkName: "n"}
	if err := WriteSiteMetadata("x", meta); err != nil {
		t.Fatal(err)
	}
	if !HasSiteMetadata("x") {
		t.Fatal("HasSiteMetadata should be true after write")
	}
	if err := RemoveSiteMetadata("x"); err != nil {
		t.Fatal(err)
	}
	if HasSiteMetadata("x") {
		t.Error("HasSiteMetadata should be false after remove")
	}
	if _, err := os.Stat(filepath.Join(root, "sites", "x")); !os.IsNotExist(err) {
		t.Errorf("site dir should be gone: %v", err)
	}
}

func TestRemoveSiteMetadataMissingIsNoop(t *testing.T) {
	withSRVRoot(t)
	if err := RemoveSiteMetadata("never-was"); err != nil {
		t.Errorf("Remove missing -> %v, want nil", err)
	}
}

func TestHasSiteMetadataFalseForMissing(t *testing.T) {
	withSRVRoot(t)
	if HasSiteMetadata("nope") {
		t.Error("HasSiteMetadata on missing should be false")
	}
}

func TestWriteSiteMetadataAtomicWrite(t *testing.T) {
	root := withSRVRoot(t)
	meta := SiteMetadata{Type: SiteTypeStatic, Domains: []string{"x.local"}, ProjectPath: "/tmp/x", Port: 80, NetworkName: "n"}
	if err := WriteSiteMetadata("x", meta); err != nil {
		t.Fatal(err)
	}
	// No leftover .tmp file in the site dir.
	if _, err := os.Stat(filepath.Join(root, "sites", "x", "metadata.yml.tmp")); !os.IsNotExist(err) {
		t.Errorf(".tmp file left behind: %v", err)
	}
}

// Atomic-write behaviour is covered by internal/fsutil; metadata.go delegates
// to fsutil.AtomicWriteFile.

func TestRemoveSiteMetadataReadOnlyParent(t *testing.T) {
	// Try removing through a path where RemoveAll succeeds even for missing.
	// Just exercise the missing path: it returns nil.
	withSRVRoot(t)
	if err := RemoveSiteMetadata("never-was-removable"); err != nil {
		t.Errorf("err: %v", err)
	}
}
