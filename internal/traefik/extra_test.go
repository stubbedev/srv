package traefik

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/config"
)

func newTraefikCfg(t *testing.T) *config.Config {
	t.Helper()
	root := t.TempDir()
	cfg := &config.Config{
		Root:        root,
		TraefikDir:  filepath.Join(root, "traefik"),
		SitesDir:    filepath.Join(root, "sites"),
		NetworkName: "tnet",
	}
	if err := os.MkdirAll(cfg.TraefikConfDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestBareDomain(t *testing.T) {
	if got := BareDomain("*.example.com"); got != "example.com" {
		t.Errorf("got %q", got)
	}
	if got := BareDomain("example.com"); got != "example.com" {
		t.Errorf("non-wildcard unchanged: %q", got)
	}
}

func TestIsWildcardEntry(t *testing.T) {
	if !IsWildcardEntry("*.foo.local") {
		t.Error("expected wildcard")
	}
	if IsWildcardEntry("foo.local") {
		t.Error("expected non-wildcard")
	}
}

func TestYamlSingleQuote(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"hello", "'hello'"},
		{"it's", "'it''s'"},
		{"", "''"},
		{"''", "''''''"},
	}
	for _, c := range cases {
		if got := yamlSingleQuote(c.in); got != c.want {
			t.Errorf("yamlSingleQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBuildHostRuleSingle(t *testing.T) {
	got := BuildHostRule([]string{"blog.local"}, false)
	if got != "Host(`blog.local`)" {
		t.Errorf("got %q", got)
	}
}

func TestBuildHostRuleWildcard(t *testing.T) {
	got := BuildHostRule([]string{"blog.local"}, true)
	if !strings.Contains(got, "Host(`blog.local`)") {
		t.Errorf("missing Host: %q", got)
	}
	if !strings.Contains(got, "HostRegexp") {
		t.Errorf("missing HostRegexp: %q", got)
	}
}

func TestBuildHostRuleMultiple(t *testing.T) {
	got := BuildHostRule([]string{"a.local", "b.local"}, false)
	if !strings.Contains(got, "Host(`a.local`)") || !strings.Contains(got, "Host(`b.local`)") {
		t.Errorf("missing host: %q", got)
	}
	if !strings.Contains(got, " || ") {
		t.Errorf("missing OR: %q", got)
	}
}

func TestBuildHostRuleSkipsEmpty(t *testing.T) {
	got := BuildHostRule([]string{"", "a.local", ""}, false)
	if got != "Host(`a.local`)" {
		t.Errorf("got %q", got)
	}
}

func TestWriteRoutesConfigEmptyRemoves(t *testing.T) {
	cfg := newTraefikCfg(t)
	path := routesConfigPath(cfg, "site")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteRoutesConfig(cfg, SiteRouteSet{SiteName: "site"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file should be removed: %v", err)
	}
}

func TestWriteRoutesConfigOne(t *testing.T) {
	cfg := newTraefikCfg(t)
	set := SiteRouteSet{
		SiteName: "site",
		Domains:  []string{"site.local"},
		IsLocal:  true,
		Routes: []RouteSpec{
			{ID: "api", Path: "/api", UpstreamURL: "http://upstream:80", PreserveHost: true},
		},
	}
	if err := WriteRoutesConfig(cfg, set); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(routesConfigPath(cfg, "site"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "PathPrefix(`/api`)") {
		t.Error("matcher missing")
	}
	if !strings.Contains(body, "http://upstream:80") {
		t.Error("upstream missing")
	}
	if !strings.Contains(body, "site-api") {
		t.Error("router id missing")
	}
}

func TestWriteRoutesConfigRewrite(t *testing.T) {
	cfg := newTraefikCfg(t)
	set := SiteRouteSet{
		SiteName: "site",
		Domains:  []string{"site.local"},
		IsLocal:  false,
		Routes: []RouteSpec{
			{ID: "v1", PathRegex: "^/v1/(.+)$", Rewrite: "/$1", UpstreamURL: "https://api.example.com"},
		},
	}
	if err := WriteRoutesConfig(cfg, set); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(routesConfigPath(cfg, "site"))
	body := string(data)
	if !strings.Contains(body, "replacePathRegex") {
		t.Error("middleware missing")
	}
	if !strings.Contains(body, "letsencrypt") {
		t.Error("non-local should reference letsencrypt resolver")
	}
}

func TestWriteRoutesConfigMissingID(t *testing.T) {
	cfg := newTraefikCfg(t)
	set := SiteRouteSet{
		SiteName: "site",
		Domains:  []string{"site.local"},
		Routes:   []RouteSpec{{Path: "/api", UpstreamURL: "http://x"}},
	}
	if err := WriteRoutesConfig(cfg, set); err == nil {
		t.Error("expected error for missing id")
	}
}

func TestWriteRoutesConfigDuplicateID(t *testing.T) {
	cfg := newTraefikCfg(t)
	set := SiteRouteSet{
		SiteName: "site",
		Domains:  []string{"site.local"},
		Routes: []RouteSpec{
			{ID: "x", Path: "/a", UpstreamURL: "http://x"},
			{ID: "x", Path: "/b", UpstreamURL: "http://x"},
		},
	}
	if err := WriteRoutesConfig(cfg, set); err == nil {
		t.Error("expected duplicate-id error")
	}
}

func TestWriteRoutesConfigBadMatcher(t *testing.T) {
	cfg := newTraefikCfg(t)
	set := SiteRouteSet{
		SiteName: "site",
		Domains:  []string{"site.local"},
		Routes:   []RouteSpec{{ID: "x", UpstreamURL: "http://x"}},
	}
	if err := WriteRoutesConfig(cfg, set); err == nil {
		t.Error("expected error for missing path/regex")
	}
}

func TestHasRoutesConfig(t *testing.T) {
	cfg := newTraefikCfg(t)
	if HasRoutesConfig(cfg, "x") {
		t.Error("should be false initially")
	}
	path := routesConfigPath(cfg, "x")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !HasRoutesConfig(cfg, "x") {
		t.Error("should be true after write")
	}
}

func TestRemoveRoutesConfig(t *testing.T) {
	cfg := newTraefikCfg(t)
	path := routesConfigPath(cfg, "x")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveRoutesConfig(cfg, "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file should be removed")
	}
	if err := RemoveRoutesConfig(cfg, "x"); err != nil {
		t.Errorf("remove of missing should be noop, got %v", err)
	}
}

func TestRoutesConfigPath(t *testing.T) {
	cfg := newTraefikCfg(t)
	got := routesConfigPath(cfg, "blog")
	if !strings.Contains(got, "blog") || !strings.HasSuffix(got, ".yml") {
		t.Errorf("got %q", got)
	}
}

func TestDockerComposeTemplateContainsCreds(t *testing.T) {
	out := DockerComposeTemplate("netname", "/sites", "user1", "pa''ss")
	if !strings.Contains(out, "'user1'") {
		t.Error("user not quoted")
	}
	if !strings.Contains(out, "'pa''''ss'") {
		t.Errorf("password not escaped: %q", out[:300])
	}
	if !strings.Contains(out, "netname") {
		t.Error("network missing")
	}
}
