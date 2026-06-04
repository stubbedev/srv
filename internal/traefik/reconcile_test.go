package traefik

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
)

func reconcileCfg(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("SRV_ROOT", t.TempDir())
	config.ResetCache()
	t.Cleanup(config.ResetCache)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg
}

func TestInstalledVersionRoundTrip(t *testing.T) {
	cfg := reconcileCfg(t)
	if v := InstalledVersion(cfg); v != "" {
		t.Errorf("fresh install marker = %q, want empty", v)
	}
	if err := MarkInstalled(cfg, "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	if v := InstalledVersion(cfg); v != "v1.2.3" {
		t.Errorf("marker = %q, want v1.2.3", v)
	}
}

func TestReconcileVersionNoOps(t *testing.T) {
	// dev placeholder → never reconciles (and never touches docker).
	if did, err := ReconcileVersion(constants.DefaultVersion); err != nil || did {
		t.Errorf("dev version: did=%v err=%v, want (false,nil)", did, err)
	}

	// Not installed (no compose file) → no-op.
	cfg := reconcileCfg(t)
	if did, err := ReconcileVersion("v9"); err != nil || did {
		t.Errorf("uninstalled: did=%v err=%v, want (false,nil)", did, err)
	}

	// Installed + marker already matches → no-op (no docker call).
	if err := os.MkdirAll(filepath.Dir(cfg.TraefikComposePath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.TraefikComposePath(), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MarkInstalled(cfg, "v9"); err != nil {
		t.Fatal(err)
	}
	if did, err := ReconcileVersion("v9"); err != nil || did {
		t.Errorf("marker match: did=%v err=%v, want (false,nil)", did, err)
	}
}
