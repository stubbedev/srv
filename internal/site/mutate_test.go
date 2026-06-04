package site

import "testing"

// seedSite writes a non-local static site so mutators exercise the
// metadata/routing path without touching mkcert or DNS.
func seedSite(t *testing.T, name string, domains []string) {
	t.Helper()
	if err := WriteSiteMetadata(name, SiteMetadata{
		Type:        SiteTypeStatic,
		Domains:     domains,
		ProjectPath: "/tmp",
		Port:        80,
		IsLocal:     false,
	}); err != nil {
		t.Fatalf("seed site: %v", err)
	}
}

func TestAddAlias(t *testing.T) {
	withSRVRoot(t)
	seedSite(t, "blog", []string{"blog.test"})

	changed, _, err := AddAlias("blog", "www.blog.test")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	meta, _ := ReadSiteMetadata("blog")
	if len(meta.Domains) != 2 || meta.Domains[1] != "www.blog.test" {
		t.Errorf("domains = %v", meta.Domains)
	}

	// Idempotent: adding the same alias again is a no-op.
	changed, _, err = AddAlias("blog", "www.blog.test")
	if err != nil || changed {
		t.Errorf("re-add should be no-op: changed=%v err=%v", changed, err)
	}

	// Negative: invalid alias and missing site.
	if _, _, err := AddAlias("blog", "bad/alias"); err == nil {
		t.Error("expected error for invalid alias")
	}
	if _, _, err := AddAlias("ghost", "x.test"); err == nil {
		t.Error("expected error for missing site")
	}
}

func TestRemoveAlias(t *testing.T) {
	withSRVRoot(t)
	seedSite(t, "blog", []string{"blog.test", "www.blog.test"})

	if _, err := RemoveAlias("blog", "www.blog.test"); err != nil {
		t.Fatal(err)
	}
	meta, _ := ReadSiteMetadata("blog")
	if len(meta.Domains) != 1 {
		t.Errorf("domains = %v", meta.Domains)
	}

	// Negative: canonical domain cannot be removed; unknown alias errors.
	if _, err := RemoveAlias("blog", "blog.test"); err == nil {
		t.Error("expected error removing canonical domain")
	}
	if _, err := RemoveAlias("blog", "nope.test"); err == nil {
		t.Error("expected error for unregistered alias")
	}
}

func TestSetInternalListener(t *testing.T) {
	withSRVRoot(t)
	seedSite(t, "blog", []string{"blog.test"})

	changed, _, err := SetInternalListener("blog", true)
	if err != nil || !changed {
		t.Fatalf("enable: changed=%v err=%v", changed, err)
	}
	// Idempotent.
	if changed, _, _ := SetInternalListener("blog", true); changed {
		t.Error("re-enable should be no-op")
	}
	if changed, _, _ := SetInternalListener("blog", false); !changed {
		t.Error("disable should change")
	}
}

func TestRemoveSite(t *testing.T) {
	withSRVRoot(t)
	// A site whose project dir does not exist is "broken", so RemoveSite skips
	// the docker/traefik teardown and just drops metadata — hermetic.
	if err := WriteSiteMetadata("blog", SiteMetadata{
		Type:        SiteTypeStatic,
		Domains:     []string{"blog.test"},
		ProjectPath: "/no/such/project/dir",
		Port:        80,
		IsLocal:     false,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := RemoveSite("blog"); err != nil {
		t.Fatalf("RemoveSite: %v", err)
	}
	if meta, _ := ReadSiteMetadata("blog"); meta != nil {
		t.Error("metadata should be gone after RemoveSite")
	}

	// Negative: removing a missing site errors.
	if _, err := RemoveSite("ghost"); err == nil {
		t.Error("expected error for missing site")
	}
}

func TestAddRemoveVolume(t *testing.T) {
	withSRVRoot(t)
	seedSite(t, "blog", []string{"blog.test"})

	if _, err := AddVolume("blog", VolumeMount{Source: "/tmp", Target: "/data"}); err != nil {
		t.Fatal(err)
	}
	// Negative: duplicate target, and /app overlap.
	if _, err := AddVolume("blog", VolumeMount{Source: "/tmp", Target: "/data"}); err == nil {
		t.Error("expected error for duplicate target")
	}
	if _, err := AddVolume("blog", VolumeMount{Source: "/tmp", Target: "/app/x"}); err == nil {
		t.Error("expected error for /app overlap")
	}

	if _, err := RemoveVolume("blog", "/data"); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveVolume("blog", "/data"); err == nil {
		t.Error("expected error removing absent volume")
	}
}
