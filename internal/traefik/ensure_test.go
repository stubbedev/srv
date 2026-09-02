package traefik

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/config"
)

func TestEnsureConfigFreshInstall(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	config.ResetCache()
	t.Cleanup(config.ResetCache)

	if err := EnsureConfig("alice@example.com"); err != nil {
		t.Fatalf("EnsureConfig err: %v", err)
	}

	for _, rel := range []string{
		"traefik/conf/traefik.yml",
		"traefik/conf/traefik-dynamic.yml",
		"traefik/docker-compose.yml",
		"traefik/dnsmasq.conf",
		"traefik/certs/acme.json",
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("expected %s, err: %v", rel, err)
		}
	}

	data, _ := os.ReadFile(filepath.Join(root, "traefik/conf/traefik.yml"))
	if !strings.Contains(string(data), "alice@example.com") {
		t.Error("traefik.yml missing email")
	}
}

func TestEnsureConfigIsIdempotent(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	config.ResetCache()
	t.Cleanup(config.ResetCache)

	if err := EnsureConfig("a@b.com"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureConfig("a@b.com"); err != nil {
		t.Fatalf("second EnsureConfig err: %v", err)
	}
}

func TestWriteOrMergeTraefikYMLFresh(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "traefik.yml")
	if err := writeOrMergeTraefikYML(path, "tnet", "x@y.com"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "tnet") {
		t.Error("network substitution missing")
	}
	if !strings.Contains(string(data), "x@y.com") {
		t.Error("email substitution missing")
	}
}

func TestWriteOrMergeTraefikYMLPreservesUserSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "traefik.yml")
	existing := `# user customisation
api:
  insecure: false
  dashboard: false
log:
  level: WARN
entryPoints:
  web:
    address: ":80"
`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeOrMergeTraefikYML(path, "tnet", "x@y.com"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	body := string(data)
	if !strings.Contains(body, "WARN") {
		t.Error("user log level not preserved")
	}
	if !strings.Contains(body, "dashboard: false") {
		t.Error("user api section not preserved")
	}
	if !strings.Contains(body, "tnet") {
		t.Error("network not merged")
	}
}

func TestWriteOrMergeTraefikYMLMalformedFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "traefik.yml")
	if err := os.WriteFile(path, []byte(":\n:\n: bad yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeOrMergeTraefikYML(path, "tnet", "x@y.com"); err == nil {
		t.Error("expected err on malformed existing file")
	}
}

func TestWriteTraefikCompose(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	config.ResetCache()
	t.Cleanup(config.ResetCache)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.TraefikDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeTraefikCompose(cfg); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfg.TraefikComposePath())
	if !strings.Contains(string(data), cfg.NetworkName) {
		t.Error("compose missing network")
	}
}

// A version-upgrade reconcile calls EnsureConfig on an existing install. It
// must not wipe the live TLS cert list, or Traefik falls back to its default
// self-signed cert and every site fails with a cert error until the next
// `srv install`.
func TestEnsureConfigPreservesDynamicCerts(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	config.ResetCache()
	t.Cleanup(config.ResetCache)

	if err := EnsureConfig("a@b.com"); err != nil {
		t.Fatal(err)
	}

	certDir := filepath.Join(root, "sites/start-local/certs")
	if err := os.MkdirAll(certDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, ext := range []string{".crt", ".key"} {
		if err := os.WriteFile(filepath.Join(certDir, "start.local"+ext), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := EnsureConfig("a@b.com"); err != nil {
		t.Fatalf("second EnsureConfig err: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "traefik/conf/traefik-dynamic.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "start.local.crt") {
		t.Errorf("EnsureConfig dropped the TLS cert list, got:\n%s", data)
	}
}
