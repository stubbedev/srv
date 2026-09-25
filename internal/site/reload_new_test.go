package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/traefik"
)

func TestReloadMissingSite(t *testing.T) {
	withSRVRoot(t)
	if _, err := Reload("ghost"); err == nil {
		t.Error("expected err for missing site")
	}
}

func TestReloadStaticIdempotent(t *testing.T) {
	root := withSRVRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "traefik", "conf"), 0o755); err != nil {
		t.Fatal(err)
	}
	meta := SiteMetadata{
		Type:        SiteTypeStatic,
		Domains:     []string{"x.local"},
		ProjectPath: projectDir,
		Port:        80,
		NetworkName: "n",
	}
	if err := WriteSiteMetadata("x", meta); err != nil {
		t.Fatal(err)
	}

	res, err := Reload("x")
	if err != nil {
		t.Fatalf("Reload err: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}

	// Second reload should short-circuit via hash match.
	res2, err := Reload("x")
	if err != nil {
		t.Fatal(err)
	}
	if !res2.Skipped {
		t.Errorf("second Reload should skip, got %+v", res2)
	}
}

func TestReloadInvalidMetadata(t *testing.T) {
	withSRVRoot(t)
	// Metadata with no domains is invalid (ValidateMetadata rejects).
	meta := SiteMetadata{
		Type:        SiteTypeStatic,
		ProjectPath: "/tmp",
		NetworkName: "n",
	}
	if err := WriteSiteMetadata("invalid", meta); err != nil {
		t.Fatal(err)
	}
	if _, err := Reload("invalid"); err == nil {
		t.Error("expected validation err")
	}
}

func TestReloadComposeWritesRouteConfig(t *testing.T) {
	root := withSRVRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "traefik", "conf"), 0o755); err != nil {
		t.Fatal(err)
	}
	meta := SiteMetadata{
		Type:        SiteTypeCompose,
		Domains:     []string{"app.local"},
		ProjectPath: projectDir,
		Port:        8080,
		ServiceName: "app-web",
		NetworkName: "n",
	}
	if err := WriteSiteMetadata("app", meta); err != nil {
		t.Fatal(err)
	}
	res, err := Reload("app")
	if err != nil {
		t.Fatalf("Reload err: %v", err)
	}
	if res.NeedsRestart {
		t.Error("compose reload should not require restart")
	}
	// Verify the routing config landed.
	confDir := filepath.Join(root, "traefik", "conf")
	if _, err := os.Stat(filepath.Join(confDir, "site-app.yml")); err != nil {
		t.Errorf("traefik route config missing: %v", err)
	}
}

func TestBuildRouteSetEmpty(t *testing.T) {
	meta := &SiteMetadata{Domains: []string{"x.local"}, Wildcard: false, IsLocal: true}
	set, err := CompileRoutes("x", meta.Domains, meta.Wildcard, meta.IsLocal, meta.Routes)
	if err != nil {
		t.Fatal(err)
	}
	if set.SiteName != "x" {
		t.Errorf("SiteName = %q", set.SiteName)
	}
	if len(set.Routes) != 0 {
		t.Errorf("expected 0 routes, got %d", len(set.Routes))
	}
}

func TestBuildRouteSetWithRoutes(t *testing.T) {
	preserve := false
	meta := &SiteMetadata{
		Domains: []string{"api.local"},
		IsLocal: true,
		Routes: []Route{
			{
				ID:           "ws",
				Path:         "/app",
				Upstream:     Upstream{Kind: "localhost", Port: 6001},
				PreserveHost: &preserve,
				Priority:     200,
			},
		},
	}
	set, err := CompileRoutes("api", meta.Domains, meta.Wildcard, meta.IsLocal, meta.Routes)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(set.Routes))
	}
	r := set.Routes[0]
	if r.ID != "ws" || r.Path != "/app" {
		t.Errorf("route fields wrong: %+v", r)
	}
	if r.PreserveHost {
		t.Error("PreserveHost should be false (explicit)")
	}
}

func TestBuildRouteSetDefaultsPreserveHostTrue(t *testing.T) {
	meta := &SiteMetadata{
		Domains: []string{"api.local"},
		Routes: []Route{
			{ID: "r1", Path: "/api", Upstream: Upstream{Kind: "localhost", Port: 80}},
		},
	}
	set, err := CompileRoutes("api", meta.Domains, meta.Wildcard, meta.IsLocal, meta.Routes)
	if err != nil {
		t.Fatal(err)
	}
	if !set.Routes[0].PreserveHost {
		t.Error("PreserveHost should default to true when unset")
	}
}

// The compiler fails loudly on a malformed upstream rather than skipping it:
// validation rejects those at the boundary, so a failure here is a bug and a
// silent skip would tear down good routes alongside the bad one.
func TestBuildRouteSetRejectsMalformedUpstream(t *testing.T) {
	meta := &SiteMetadata{
		Domains: []string{"api.local"},
		Routes: []Route{
			{ID: "bad", Path: "/api", Upstream: Upstream{Kind: "unknown"}},
		},
	}
	if _, err := CompileRoutes("api", meta.Domains, meta.Wildcard, meta.IsLocal, meta.Routes); err == nil {
		t.Error("expected error for unknown upstream kind")
	}
}

func TestValidateMetadataNilFails(t *testing.T) {
	if err := ValidateMetadata(nil); err == nil {
		t.Error("expected err on nil meta")
	}
}

func TestValidateMetadataNoDomainsFails(t *testing.T) {
	if err := ValidateMetadata(&SiteMetadata{Type: SiteTypeStatic}); err == nil {
		t.Error("expected err for no domains")
	}
}

func TestValidateMetadataEmptyDomain(t *testing.T) {
	meta := &SiteMetadata{Domains: []string{""}}
	if err := ValidateMetadata(meta); err == nil {
		t.Error("expected err for empty domain entry")
	}
}

func TestValidateMetadataDuplicateDomain(t *testing.T) {
	meta := &SiteMetadata{Domains: []string{"a.local", "a.local"}}
	if err := ValidateMetadata(meta); err == nil {
		t.Error("expected err for duplicate domains")
	}
}

func TestValidateMetadataUnknownListener(t *testing.T) {
	meta := &SiteMetadata{Domains: []string{"a.local"}, Listeners: []string{"weird"}}
	if err := ValidateMetadata(meta); err == nil {
		t.Error("expected err for unknown listener")
	}
}

func TestValidateMetadataRouteMissingID(t *testing.T) {
	meta := &SiteMetadata{
		Domains: []string{"a.local"},
		Routes:  []Route{{Path: "/", Upstream: Upstream{Kind: "localhost", Port: 80}}},
	}
	if err := ValidateMetadata(meta); err == nil {
		t.Error("expected err for missing route id")
	}
}

func TestValidateMetadataRouteBadID(t *testing.T) {
	meta := &SiteMetadata{
		Domains: []string{"a.local"},
		Routes:  []Route{{ID: "Bad ID", Path: "/", Upstream: Upstream{Kind: "localhost", Port: 80}}},
	}
	if err := ValidateMetadata(meta); err == nil {
		t.Error("expected err for invalid route id")
	}
}

func TestValidateMetadataRouteBothPathAndRegex(t *testing.T) {
	meta := &SiteMetadata{
		Domains: []string{"a.local"},
		Routes:  []Route{{ID: "x", Path: "/a", PathRegex: "/b", Upstream: Upstream{Kind: "localhost", Port: 80}}},
	}
	if err := ValidateMetadata(meta); err == nil {
		t.Error("expected err for both path and regex")
	}
}

func TestValidateMetadataRouteBadRegex(t *testing.T) {
	meta := &SiteMetadata{
		Domains: []string{"a.local"},
		Routes:  []Route{{ID: "x", PathRegex: "(?P<", Upstream: Upstream{Kind: "localhost", Port: 80}}},
	}
	if err := ValidateMetadata(meta); err == nil {
		t.Error("expected err for invalid regex")
	}
}

func TestValidateMetadataRouteRewriteNeedsRegex(t *testing.T) {
	meta := &SiteMetadata{
		Domains: []string{"a.local"},
		Routes:  []Route{{ID: "x", Path: "/a", Rewrite: "/b", Upstream: Upstream{Kind: "localhost", Port: 80}}},
	}
	if err := ValidateMetadata(meta); err == nil {
		t.Error("expected err: rewrite without regex")
	}
}

func TestValidateMetadataRouteMissingUpstreamKind(t *testing.T) {
	meta := &SiteMetadata{
		Domains: []string{"a.local"},
		Routes:  []Route{{ID: "x", Path: "/a"}},
	}
	if err := ValidateMetadata(meta); err == nil {
		t.Error("expected err: missing upstream kind")
	}
}

func TestValidateMetadataRouteBadUpstreamKind(t *testing.T) {
	meta := &SiteMetadata{
		Domains: []string{"a.local"},
		Routes:  []Route{{ID: "x", Path: "/a", Upstream: Upstream{Kind: "weird"}}},
	}
	if err := ValidateMetadata(meta); err == nil {
		t.Error("expected err: unknown upstream kind")
	}
}

func TestValidateMetadataValidComplete(t *testing.T) {
	meta := &SiteMetadata{
		Domains:   []string{"a.local"},
		Listeners: []string{"internal"},
		Routes: []Route{
			{ID: "r1", Path: "/", Upstream: Upstream{Kind: "localhost", Port: 80}},
			{ID: "r2", PathRegex: "^/v1/(.+)$", Rewrite: "/$1", Upstream: Upstream{Kind: "url", URL: "https://api.example.com"}},
		},
	}
	if err := ValidateMetadata(meta); err != nil {
		t.Errorf("unexpected err: %v", err)
	}
}

// Hostile metadata.yml values must be rejected at this choke point: domains
// and container names are interpolated verbatim into dnsmasq conf lines, the
// hosts file, and Traefik Host() rules, where a newline or space would inject
// directives or break out of the rule quoting.
func TestValidateMetadataRejectsInjectableDomain(t *testing.T) {
	for _, d := range []string{"a.local\naddress=/evil.local/9.9.9.9", "a b.local", "a\t.local", "a`b.local"} {
		meta := &SiteMetadata{Domains: []string{d}}
		if err := ValidateMetadata(meta); err == nil {
			t.Errorf("expected err for injectable domain %q", d)
		}
	}
}

func TestValidateMetadataRejectsInjectableServiceName(t *testing.T) {
	meta := &SiteMetadata{Domains: []string{"a.local"}, ServiceName: "web\nports:\n  - 1:1"}
	if err := ValidateMetadata(meta); err == nil {
		t.Error("expected err for injectable service name")
	}
}

func TestValidateMetadataRejectsBadUpstreamURL(t *testing.T) {
	meta := &SiteMetadata{
		Domains: []string{"a.local"},
		Routes:  []Route{{ID: "x", Path: "/a", Upstream: Upstream{Kind: "url", URL: "http://"}}},
	}
	if err := ValidateMetadata(meta); err == nil {
		t.Error("expected err for upstream url without host")
	}
}

func TestValidateMetadataRejectsInjectableRouteContainer(t *testing.T) {
	meta := &SiteMetadata{
		Domains: []string{"a.local"},
		Routes:  []Route{{ID: "x", Path: "/a", Upstream: Upstream{Kind: "container", Container: "c\nextra", Port: 80}}},
	}
	if err := ValidateMetadata(meta); err == nil {
		t.Error("expected err for injectable route container name")
	}
}

func TestValidateMetadataRejectsOutOfRangePort(t *testing.T) {
	meta := &SiteMetadata{Domains: []string{"a.local"}, Port: 70000}
	if err := ValidateMetadata(meta); err == nil {
		t.Error("expected err for out-of-range port")
	}
}

// Confirm traefik import is still referenced (silences lint).
var _ = traefik.LocalDomains

func TestReloadDockerfile(t *testing.T) {
	root := withSRVRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "traefik", "conf"), 0o755); err != nil {
		t.Fatal(err)
	}
	meta := SiteMetadata{
		Type:           SiteTypeDockerfile,
		Domains:        []string{"app.local"},
		ProjectPath:    projectDir,
		Port:           3000,
		NetworkName:    "n",
		DockerfilePort: 3000,
	}
	if err := WriteSiteMetadata("app", meta); err != nil {
		t.Fatal(err)
	}
	res, err := Reload("app")
	if err != nil {
		t.Fatal(err)
	}
	if !res.NeedsRestart {
		t.Error("Dockerfile reload should require restart")
	}
}
