package metrics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/config"
)

func setLinux(t *testing.T, v bool) {
	t.Helper()
	prev := isLinux
	isLinux = func() bool { return v }
	t.Cleanup(func() { isLinux = prev })
}

func newCfg(t *testing.T) *config.Config {
	t.Helper()
	root := t.TempDir()
	return &config.Config{
		Root:        root,
		TraefikDir:  filepath.Join(root, "traefik"),
		SitesDir:    filepath.Join(root, "sites"),
		NetworkName: "tnet",
	}
}

func TestIsConfigured(t *testing.T) {
	cfg := newCfg(t)
	// Negative: nothing written yet.
	if IsConfigured(cfg) {
		t.Error("IsConfigured should be false before the stack is written")
	}
	// Positive: compose file present.
	if err := os.MkdirAll(Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ComposePath(cfg), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !IsConfigured(cfg) {
		t.Error("IsConfigured should be true once docker-compose.yml exists")
	}
}

func TestDir(t *testing.T) {
	cfg := newCfg(t)
	if got, want := Dir(cfg), filepath.Join(cfg.Root, "metrics"); got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
}

func TestTraefikConfigPath(t *testing.T) {
	cfg := newCfg(t)
	got := TraefikConfigPath(cfg)
	if !strings.Contains(got, "metrics") {
		t.Errorf("TraefikConfigPath = %q", got)
	}
	if !strings.HasSuffix(got, ".yml") && !strings.HasSuffix(got, ".yaml") {
		t.Errorf("TraefikConfigPath = %q, expected yaml ext", got)
	}
}

func TestWriteTraefikConfigLinux(t *testing.T) {
	setLinux(t, true)
	cfg := newCfg(t)
	if err := os.MkdirAll(cfg.TraefikConfDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteTraefikConfig(cfg); err != nil {
		t.Fatalf("WriteTraefikConfig err: %v", err)
	}
	data, err := os.ReadFile(TraefikConfigPath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "127.0.0.1") {
		t.Error("Linux template should target loopback")
	}
	if strings.Contains(body, GrafanaContainer+":3000") {
		t.Error("Linux template must not use container name")
	}
}

func TestWriteTraefikConfigDarwin(t *testing.T) {
	setLinux(t, false)
	cfg := newCfg(t)
	if err := os.MkdirAll(cfg.TraefikConfDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteTraefikConfig(cfg); err != nil {
		t.Fatalf("WriteTraefikConfig err: %v", err)
	}
	data, err := os.ReadFile(TraefikConfigPath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, GrafanaContainer) {
		t.Error("non-Linux template should use container name")
	}
	if !strings.Contains(body, PrometheusContainer) {
		t.Error("non-Linux template should use prometheus container name")
	}
}

func TestRemoveTraefikConfigMissingIsNoop(t *testing.T) {
	cfg := newCfg(t)
	if err := RemoveTraefikConfig(cfg); err != nil {
		t.Errorf("RemoveTraefikConfig on missing path err: %v", err)
	}
}

func TestRemoveTraefikConfigExisting(t *testing.T) {
	cfg := newCfg(t)
	if err := os.MkdirAll(cfg.TraefikConfDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	path := TraefikConfigPath(cfg)
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveTraefikConfig(cfg); err != nil {
		t.Errorf("RemoveTraefikConfig err: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file should be gone, stat err: %v", err)
	}
}

func TestWriteStackLinux(t *testing.T) {
	setLinux(t, true)
	cfg := newCfg(t)
	if err := WriteStack(cfg); err != nil {
		t.Fatalf("WriteStack err: %v", err)
	}
	dir := Dir(cfg)
	for _, rel := range []string{"prometheus.yml", "docker-compose.yml", filepath.Join("grafana-provisioning", "datasources", "prometheus.yml")} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}
	compose, _ := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if !strings.Contains(string(compose), "network_mode: host") {
		t.Error("Linux compose should use host networking")
	}
}

func TestWriteStackDarwin(t *testing.T) {
	setLinux(t, false)
	cfg := newCfg(t)
	if err := WriteStack(cfg); err != nil {
		t.Fatalf("WriteStack err: %v", err)
	}
	compose, _ := os.ReadFile(filepath.Join(Dir(cfg), "docker-compose.yml"))
	body := string(compose)
	if strings.Contains(body, "network_mode: host") {
		t.Error("non-Linux compose should NOT use host networking")
	}
	if !strings.Contains(body, "external: true") {
		t.Error("non-Linux compose should reference external network")
	}
}

func TestWriteStackMkdirErr(t *testing.T) {
	// Block dir creation by making the Root a file.
	tmp := t.TempDir()
	bad := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(bad, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Root: bad, NetworkName: "n"}
	if err := WriteStack(cfg); err == nil {
		t.Error("expected mkdir err")
	}
}

func TestPrometheusYAMLLinuxTarget(t *testing.T) {
	setLinux(t, true)
	if !strings.Contains(prometheusYAML(), "127.0.0.1:8080") {
		t.Error("Linux scrape target wrong")
	}
}

func TestPrometheusYAMLDarwinTarget(t *testing.T) {
	setLinux(t, false)
	if !strings.Contains(prometheusYAML(), "srv-traefik:8080") {
		t.Error("non-Linux scrape target wrong")
	}
}

func TestGrafanaDatasourceURL(t *testing.T) {
	setLinux(t, true)
	if !strings.Contains(grafanaDatasourceYAML(), "127.0.0.1") {
		t.Error("Linux grafana DS URL wrong")
	}
	setLinux(t, false)
	if !strings.Contains(grafanaDatasourceYAML(), "srv-prometheus:9090") {
		t.Error("non-Linux grafana DS URL wrong")
	}
}

func TestRemoveTraefikConfigPermError(t *testing.T) {
	// Make the file path a non-empty directory so os.Remove fails non-IsNotExist.
	cfg := newCfg(t)
	if err := os.MkdirAll(cfg.TraefikConfDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	path := TraefikConfigPath(cfg)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "blocker"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveTraefikConfig(cfg); err == nil {
		t.Error("expected err removing non-empty dir")
	}
}

func TestComposeYAMLBranches(t *testing.T) {
	setLinux(t, true)
	if !strings.Contains(composeYAML("n"), "network_mode: host") {
		t.Error("Linux branch failed")
	}
	setLinux(t, false)
	if !strings.Contains(composeYAML("net1"), "name: net1") {
		t.Error("non-Linux branch should reference network name")
	}
}
