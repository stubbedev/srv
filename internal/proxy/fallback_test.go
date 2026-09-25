package proxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
)

func TestFallbackDir(t *testing.T) {
	cfg := &config.Config{SitesDir: "/srv/sites"}
	if got := FallbackDir(cfg, "blog"); got != "/srv/sites/_proxy-blog-fallback" {
		t.Errorf("got %q", got)
	}
}

func TestValidateFallbackURL(t *testing.T) {
	valid := []string{
		"https://prod.example.com",
		"http://prod.example.com:8080/path",
		"https://1.2.3.4",
	}
	for _, in := range valid {
		if err := validateFallbackURL(in); err != nil {
			t.Errorf("validateFallbackURL(%q) = %v, want nil", in, err)
		}
	}
	invalid := []string{
		"",
		"prod.example.com",       // no scheme
		"ftp://prod.example.com", // wrong scheme
		"https://",               // no host
		"://nope",                // unparseable
	}
	for _, in := range invalid {
		if err := validateFallbackURL(in); err == nil {
			t.Errorf("validateFallbackURL(%q) = nil, want an error", in)
		}
	}
}

func TestParseSemverMinor(t *testing.T) {
	cases := []struct {
		in           string
		major, minor int
		ok           bool
	}{
		{"3.7.9", 3, 7, true},
		{"v3.7.9", 3, 7, true},
		{"traefik:v3.7", 3, 7, true},
		{"2.11.0", 2, 11, true},
		{"latest", 0, 0, false},
		{"", 0, 0, false},
		{"3", 0, 0, false},
		{"v3.x.9", 0, 0, false},
	}
	for _, c := range cases {
		major, minor, ok := parseSemverMinor(c.in)
		if ok != c.ok || major != c.major || minor != c.minor {
			t.Errorf("parseSemverMinor(%q) = %d.%d ok=%v, want %d.%d ok=%v", c.in, major, minor, ok, c.major, c.minor, c.ok)
		}
	}
}

// setupLegacyProxy writes proxy metadata in the shape pre-native-failover srv
// produced and returns the SRV_ROOT config it lives under.
func setupLegacyProxy(t *testing.T, meta Metadata) *config.Config {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("SRV_ROOT", tmp)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("SRV_ROOT") })
	if err := Write(meta); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestMigrateLegacyFallbackDaemonListener(t *testing.T) {
	cfg := setupLegacyProxy(t, Metadata{
		Name:         "app",
		Domains:      []string{"app.test"},
		IsLocal:      true,
		Port:         3000,
		FallbackURL:  "https://prod.example.com",
		FallbackPort: 41234,
	})
	if err := os.MkdirAll(cfg.TraefikConfDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	if warns := MigrateLegacyFallback(cfg, mustMeta(t, "app")); len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}

	route := filepath.Join(cfg.TraefikConfDir(), "proxy-app.yml")
	data, err := os.ReadFile(route)
	if err != nil {
		t.Fatalf("read rendered route: %v", err)
	}
	for _, want := range []string{
		"failover:", "service: proxy-app-primary", "fallback: proxy-app-fallback",
		"500-599", "dialTimeout: 2s", "url: http://localhost:3000",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("rendered route missing %q:\n%s", want, data)
		}
	}
	// The failover wrapper itself must carry no loadBalancer block: Traefik
	// rejects a service with two types. Assert on the parsed document rather
	// than string offsets, since the router shares the service's name.
	var doc struct {
		HTTP struct {
			Services map[string]map[string]any `yaml:"services"`
		} `yaml:"http"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse rendered route: %v", err)
	}
	wrapper, ok := doc.HTTP.Services["proxy-app"]
	if !ok {
		t.Fatalf("no proxy-app service in:\n%s", data)
	}
	if _, has := wrapper["failover"]; !has {
		t.Errorf("proxy-app is not a failover service:\n%s", data)
	}
	if _, has := wrapper["loadBalancer"]; has {
		t.Errorf("failover wrapper must not carry its own loadBalancer:\n%s", data)
	}

	meta := mustMeta(t, "app")
	if meta.FallbackPort != 0 {
		t.Errorf("FallbackPort = %d, want cleared", meta.FallbackPort)
	}
	if meta.FallbackURL != "https://prod.example.com" {
		t.Errorf("FallbackURL = %q, want preserved", meta.FallbackURL)
	}
}

func TestMigrateLegacyFallbackSidecar(t *testing.T) {
	cfg := setupLegacyProxy(t, Metadata{
		Name:         "web",
		Domains:      []string{"web.test"},
		IsLocal:      true,
		FallbackURL:  "https://prod.example.com",
		FallbackPort: 0,
	})
	// The retired container-primary sidecar hid the real primary in its
	// nginx.conf proxy_pass.
	dir := FallbackDir(cfg, "web")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	nginxConf := "location / {\n    proxy_pass http://myapp:3000;\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "nginx.conf"), []byte(nginxConf), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.MkdirAll(cfg.TraefikConfDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	// Hermetic teardown: the migration's sidecar removal shells out to
	// `docker compose down`; the swap keeps the unit test off the engine.
	restoreCompose := docker.SwapComposeExec(func(string, bool, ...string) error { return nil })
	t.Cleanup(restoreCompose)

	if warns := MigrateLegacyFallback(cfg, mustMeta(t, "web")); len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}

	data, err := os.ReadFile(filepath.Join(cfg.TraefikConfDir(), "proxy-web.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "url: http://myapp:3000") {
		t.Errorf("primary not recovered from sidecar proxy_pass:\n%s", data)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("sidecar dir still exists after migration: %v", err)
	}
}

func TestMigrateLegacyFallbackNoop(t *testing.T) {
	cfg := setupLegacyProxy(t, Metadata{
		Name:    "plain",
		Domains: []string{"plain.test"},
		IsLocal: true,
		Port:    8080,
	})
	if warns := MigrateLegacyFallback(cfg, mustMeta(t, "plain")); len(warns) != 0 {
		t.Fatalf("no legacy hop, got warnings: %v", warns)
	}
	if _, err := os.Stat(filepath.Join(cfg.TraefikConfDir(), "proxy-plain.yml")); !os.IsNotExist(err) {
		t.Errorf("route rendered for a proxy without a fallback: %v", err)
	}
}

func mustMeta(t *testing.T, name string) *Metadata {
	t.Helper()
	meta, err := Read(name)
	if err != nil || meta == nil {
		t.Fatalf("read %s metadata: %v (%v)", name, meta, err)
	}
	return meta
}
