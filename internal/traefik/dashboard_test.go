package traefik

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/mkcert"
	"github.com/stubbedev/srv/internal/shell/shelltest"
)

func TestWriteDashboardProxyConfig(t *testing.T) {
	cfg := newTraefikCfg(t)
	if err := writeDashboardProxyConfig(cfg, "traefik", constants.TraefikDashboardDomain); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.TraefikConfDir(), "proxy-traefik.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), constants.TraefikDashboardDomain) {
		t.Error("config missing dashboard domain")
	}
}

func TestSetupDashboardProxyHappy(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	config.ResetCache()
	t.Cleanup(config.ResetCache)
	if err := os.MkdirAll(filepath.Join(root, "traefik", "conf"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mkcert.SwapRunner(mkcertHappyStub{}))
	swapShell(t, shelltest.New(nil))
	if err := SetupDashboardProxy(); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestCheckPortConflicts(t *testing.T) {
	swapShell(t, shelltest.New(nil))
	_ = CheckPortConflicts() // just exercise it; result depends on host
}

// mkcertHappyStub satisfies mkcert.CommandRunner with successful defaults.
type mkcertHappyStub struct{}

func (mkcertHappyStub) Stream(args ...string) error           { return nil }
func (mkcertHappyStub) Output(args ...string) ([]byte, error) { return []byte("/tmp/ca\n"), nil }
func (mkcertHappyStub) Combined(args ...string) ([]byte, error) {
	return []byte("Created a new local CA"), nil
}
