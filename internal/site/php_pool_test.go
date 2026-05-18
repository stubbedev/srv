package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/docker"
)

func TestCollectPoolMembersSingleSite(t *testing.T) {
	withSRVRoot(t)
	members, err := collectPoolMembers("fp1", "blog", "/srv/blog")
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(members))
	}
	if members[0].SiteName != "blog" || members[0].ProjectPath != "/srv/blog" {
		t.Errorf("got %+v", members[0])
	}
}

func TestCollectPoolMembersFindsSameFingerprint(t *testing.T) {
	withSRVRoot(t)
	// Register a second PHP site with the same PHPVersion + extensions.
	writeTestSite_php(t, "other", "8.3", []string{"redis"})
	writeTestSite_php(t, "different", "8.4", []string{"redis"})

	exts := []string{"redis"}
	info := &PHPSiteInfo{PHPVersion: "8.3", Extensions: exts}
	// Match the fingerprint scheme.
	fp := fingerprintFor(info)

	members, err := collectPoolMembers(fp, "blog", "/srv/blog")
	if err != nil {
		t.Fatal(err)
	}
	// blog + other share fp; different does not.
	names := map[string]bool{}
	for _, m := range members {
		names[m.SiteName] = true
	}
	if !names["blog"] {
		t.Error("blog missing")
	}
	if !names["other"] {
		t.Error("other (same fingerprint) missing")
	}
	if names["different"] {
		t.Error("different (other fp) should not be in members")
	}
}

func TestCollectPoolMembersSkipsUnderscore(t *testing.T) {
	root := withSRVRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "sites", "_proxy"), 0o755); err != nil {
		t.Fatal(err)
	}
	members, err := collectPoolMembers("fp1", "blog", "/srv/blog")
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 {
		t.Errorf("underscore dir should be skipped, got %v", members)
	}
}

func TestWritePHPSiteConfigCreatesFiles(t *testing.T) {
	root := withSRVRoot(t)
	projectDir := filepath.Join(root, "blog")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Stub docker compose so ensurePoolForSite doesn't shell out.
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))

	meta := SiteMetadata{
		Type:        SiteTypePHP,
		Domains:     []string{"blog.local"},
		ProjectPath: projectDir,
		Port:        80,
		IsLocal:     true,
		NetworkName: "n",
	}
	info := &PHPSiteInfo{PHPVersion: "8.3", Extensions: []string{"redis"}}
	if err := WritePHPSiteConfig("blog", meta, info, true); err != nil {
		t.Fatalf("err: %v", err)
	}
	siteDir := filepath.Join(root, "sites", "blog")
	for _, f := range []string{"nginx.conf", "docker-compose.yml"} {
		if _, err := os.Stat(filepath.Join(siteDir, f)); err != nil {
			t.Errorf("%s missing: %v", f, err)
		}
	}
}

func TestWritePHPDockerConfig(t *testing.T) {
	root := withSRVRoot(t)
	projectDir := filepath.Join(root, "blog")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))

	meta := SiteMetadata{
		Type:        SiteTypePHP,
		Domains:     []string{"blog.local"},
		ProjectPath: projectDir,
		Port:        80,
		IsLocal:     true,
		NetworkName: "n",
	}
	info := &PHPSiteInfo{PHPVersion: "8.3", Extensions: []string{"redis"}}
	// WritePHPSiteConfig creates the per-site dir; WritePHPDockerConfig
	// is the regenerate-after-version-change path that assumes it already exists.
	if err := WritePHPSiteConfig("blog", meta, info, true); err != nil {
		t.Fatal(err)
	}
	if err := WritePHPDockerConfig("blog", meta, info); err != nil {
		t.Fatalf("err: %v", err)
	}
	composePath := filepath.Join(root, "sites", "blog", "docker-compose.yml")
	if _, err := os.Stat(composePath); err != nil {
		t.Errorf("compose missing: %v", err)
	}
}

func TestRemoveSiteFromPoolEmptyTearsDown(t *testing.T) {
	withSRVRoot(t)
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))

	writeTestSite_php(t, "solo", "8.3", []string{"redis"})

	if err := RemoveSiteFromPool("solo"); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRemoveSiteFromPoolMissingMeta(t *testing.T) {
	withSRVRoot(t)
	if err := RemoveSiteFromPool("ghost"); err != nil {
		t.Errorf("missing meta should be no-op: %v", err)
	}
}

func TestRemoveSiteFromPoolNonPHP(t *testing.T) {
	withSRVRoot(t)
	writeTestSiteWith(t, "x", SiteMetadata{
		Type: SiteTypeStatic, Domains: []string{"x.local"}, ProjectPath: "/tmp", Port: 80, NetworkName: "n",
	})
	if err := RemoveSiteFromPool("x"); err != nil {
		t.Errorf("non-php should be no-op: %v", err)
	}
}

func TestRemoveSiteFromPoolWithSiblings(t *testing.T) {
	withSRVRoot(t)
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	writeTestSite_php(t, "a", "8.3", []string{"redis"})
	writeTestSite_php(t, "b", "8.3", []string{"redis"})

	// Remove one; pool should be rewritten with remaining member.
	if err := RemoveSiteFromPool("a"); err != nil {
		t.Errorf("err: %v", err)
	}
}

// writeTestSite_php is a convenience helper that creates a PHP site with the
// supplied version + extensions and a real project dir.
func writeTestSite_php(t *testing.T, name, version string, exts []string) {
	t.Helper()
	root, _ := os.UserHomeDir()
	_ = root
	projectDir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestSiteWith(t, name, SiteMetadata{
		Type:          SiteTypePHP,
		Domains:       []string{name + ".local"},
		ProjectPath:   projectDir,
		Port:          80,
		IsLocal:       true,
		NetworkName:   "n",
		PHPVersion:    version,
		PHPExtensions: exts,
	})
}

func writeTestSiteWith(t *testing.T, name string, meta SiteMetadata) {
	t.Helper()
	if err := WriteSiteMetadata(name, meta); err != nil {
		t.Fatal(err)
	}
}

// fingerprintFor mirrors the production fingerprint calculation in
// ensurePoolForSite so tests can match it.
func fingerprintFor(info *PHPSiteInfo) string {
	// Use the same pool.Fingerprint via the public path.
	return PHPImageFingerprint(info)[len("srv-php:"):]
}
