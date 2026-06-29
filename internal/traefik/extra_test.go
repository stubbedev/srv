package traefik

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

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

// Credential safety is now structural (yaml.Marshal of a typed model), so the
// behaviour is asserted by the round-trip test TestDockerComposeTemplateCreds.

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

func TestWriteRoutesConfigInsecureSkipVerify(t *testing.T) {
	cfg := newTraefikCfg(t)
	set := SiteRouteSet{
		SiteName: "site",
		Domains:  []string{"site.local"},
		IsLocal:  true,
		Routes: []RouteSpec{
			{ID: "api", Path: "/", UpstreamURL: "https://192.168.0.1:8443", InsecureSkipVerify: true},
			{ID: "plain", Path: "/plain", UpstreamURL: "http://upstream:80"},
		},
	}
	if err := WriteRoutesConfig(cfg, set); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(routesConfigPath(cfg, "site"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, "serversTransport: site-api-insecure") {
		t.Error("service should reference its serversTransport")
	}
	if !strings.Contains(s, "insecureSkipVerify: true") {
		t.Error("serversTransports block missing")
	}
	// The route without the flag must not get a transport reference.
	if strings.Contains(s, "site-plain-insecure") {
		t.Error("plain route should not have a serversTransport")
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

// composeDoc parses a rendered docker-compose string for assertions.
type composeDoc struct {
	Services map[string]struct {
		Environment []string `yaml:"environment"`
		Volumes     []string `yaml:"volumes"`
	} `yaml:"services"`
	Networks map[string]struct {
		Name string `yaml:"name"`
	} `yaml:"networks"`
}

// TestDockerComposeTemplateCreds: positive — ordinary values land in the right
// fields; negative — credentials and a sites path containing YAML-hostile
// characters round-trip verbatim and cannot break the document or inject keys.
func TestDockerComposeTemplateCreds(t *testing.T) {
	const (
		user = "user1"
		// Quotes, colon, newline, and a fake env var — all YAML-hostile.
		pass     = "p:a\"s'’s\n- INJECTED=1"
		sitesDir = "/sites:with\"quote"
		network  = "net'name"
	)
	out, err := DockerComposeTemplate(network, sitesDir, user, pass)
	if err != nil {
		t.Fatal(err)
	}

	var doc composeDoc
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("hostile values broke the document: %v\n%s", err, out)
	}

	dns := doc.Services["dns"]
	wantEnv := []string{"HTTP_USER=" + user, "HTTP_PASS=" + pass}
	if len(dns.Environment) != 2 || dns.Environment[0] != wantEnv[0] || dns.Environment[1] != wantEnv[1] {
		t.Errorf("env not round-tripped:\ngot:  %q\nwant: %q", dns.Environment, wantEnv)
	}
	// The injected "- INJECTED=1" line must be part of the password scalar, not
	// a sibling list element.
	if len(dns.Environment) != 2 {
		t.Errorf("password leaked into an extra environment entry: %q", dns.Environment)
	}
	if doc.Networks["traefik"].Name != network {
		t.Errorf("network name = %q, want %q", doc.Networks["traefik"].Name, network)
	}
	traefik := doc.Services["traefik"]
	wantVol := sitesDir + ":/etc/traefik/sites:ro"
	found := false
	for _, v := range traefik.Volumes {
		if v == wantVol {
			found = true
		}
	}
	if !found {
		t.Errorf("sites volume not round-tripped, want %q in %q", wantVol, traefik.Volumes)
	}
}
