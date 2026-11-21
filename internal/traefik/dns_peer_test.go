package traefik

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/shell/shelltest"
)

func writeUserConfig(t *testing.T, body string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	config.ResetCache()
	t.Cleanup(config.ResetCache)
	if err := os.WriteFile(filepath.Join(root, constants.UserConfigFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func resolvedWrite(t *testing.T) string {
	t.Helper()
	fake := shelltest.New(nil)
	swapShell(t, fake)
	if err := updateSystemdResolvedConfig(nil); err != nil {
		t.Fatal(err)
	}
	for _, c := range fake.Snapshot() {
		if c.Method == "SudoWrite" {
			return c.Stdin
		}
	}
	t.Fatal("updateSystemdResolvedConfig wrote nothing")
	return ""
}

func TestPeerNameRoutesHomeArpa(t *testing.T) {
	writeUserConfig(t, "peer_name: laptop\n")
	if got := resolvedWrite(t); !strings.Contains(got, "Domains=~test ~localhost ~home.arpa\n") {
		t.Errorf("with peer_name set, resolved config = %q, want ~home.arpa routed", got)
	}
}

func TestNoPeerNameLeavesHomeArpaAlone(t *testing.T) {
	writeUserConfig(t, "")
	if got := resolvedWrite(t); strings.Contains(got, "home.arpa") {
		t.Errorf("without peer_name, resolved config = %q, want no home.arpa route", got)
	}
}

func TestInvalidPeerNameFailsRouting(t *testing.T) {
	writeUserConfig(t, "peer_name: Not Valid\n")
	fake := shelltest.New(nil)
	swapShell(t, fake)
	if err := updateSystemdResolvedConfig(nil); err == nil {
		t.Error("invalid peer_name: updateSystemdResolvedConfig = nil, want error")
	}
}
