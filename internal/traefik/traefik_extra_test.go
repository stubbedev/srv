package traefik

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/shell/shelltest"
)

func TestDashboardURLs(t *testing.T) {
	if got := DashboardURL(); !strings.Contains(got, ":8080") {
		t.Errorf("DashboardURL = %q", got)
	}
	if got := DashboardLocalURL(); !strings.HasPrefix(got, "https://") {
		t.Errorf("DashboardLocalURL = %q", got)
	}
}

func TestSaveAndGetEmail(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	config.ResetCache()
	t.Cleanup(config.ResetCache)
	if err := os.MkdirAll(filepath.Join(root, "traefik"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := SaveEmail("foo@example.com"); err != nil {
		t.Fatal(err)
	}
	got, err := GetEmail(false)
	if err != nil {
		t.Fatal(err)
	}
	if got != "foo@example.com" {
		t.Errorf("got %q", got)
	}
}

func TestGetEmailMissingNoPrompt(t *testing.T) {
	t.Setenv("SRV_ROOT", t.TempDir())
	config.ResetCache()
	t.Cleanup(config.ResetCache)
	if _, err := GetEmail(false); err == nil {
		t.Error("expected err when no email and prompt=false")
	}
}

func TestPortConflictStopHintKnownProcess(t *testing.T) {
	cases := []string{"nginx", "apache2", "httpd", "caddy", "lighttpd"}
	for _, p := range cases {
		c := PortConflict{Port: 80, Process: p}
		if !strings.Contains(c.StopHint(), "systemctl stop "+p) {
			t.Errorf("StopHint for %s = %q", p, c.StopHint())
		}
	}
}

func TestPortConflictStopHintUnknownProcess(t *testing.T) {
	c := PortConflict{Port: 80, Process: "weirdservice"}
	if !strings.Contains(c.StopHint(), "systemctl stop weirdservice") {
		t.Errorf("got %q", c.StopHint())
	}
}

func TestPortConflictStopHintEmptyProcess(t *testing.T) {
	c := PortConflict{Port: 80}
	if !strings.Contains(c.StopHint(), "identify and stop") {
		t.Errorf("got %q", c.StopHint())
	}
}

func TestPortConflictCanAutoFix(t *testing.T) {
	for _, p := range []string{"nginx", "apache2", "httpd", "caddy", "lighttpd"} {
		if !(PortConflict{Process: p}).CanAutoFix() {
			t.Errorf("CanAutoFix(%q) = false", p)
		}
	}
	if (PortConflict{Process: "other"}).CanAutoFix() {
		t.Error("CanAutoFix should be false for unknown process")
	}
}

func TestPortConflictAutoFix(t *testing.T) {
	swapShell(t, shelltest.New(nil))
	if err := (PortConflict{Process: "nginx"}).AutoFix(); err != nil {
		t.Errorf("err: %v", err)
	}
	if err := (PortConflict{Process: "unknown"}).AutoFix(); err == nil {
		t.Error("expected err for unknown process")
	}
}

func TestCheckPortAvailable(t *testing.T) {
	swapShell(t, &portFakeRunner{inUse: false})
	if !CheckPortAvailable(50000) {
		t.Error("expected available")
	}
	swapShell(t, &portFakeRunner{inUse: true})
	if CheckPortAvailable(50000) {
		t.Error("expected unavailable")
	}
}

// portFakeRunner implements just enough of shell.Runner to control CheckPort.
type portFakeRunner struct {
	inUse bool
	*shelltest.Fake
}

func (p *portFakeRunner) CheckPort(string) (bool, error)               { return p.inUse, nil }
func (p *portFakeRunner) CheckPortOnAddr(string, string) (bool, error) { return p.inUse, nil }
func (p *portFakeRunner) Run(name string, args ...string) error        { return nil }
func (p *portFakeRunner) RunWithContext(context.Context, string, ...string) error {
	return nil
}
func (p *portFakeRunner) RunQuiet(string, ...string) ([]byte, error) { return nil, nil }
func (p *portFakeRunner) RunQuietWithContext(context.Context, string, ...string) ([]byte, error) {
	return nil, nil
}
func (p *portFakeRunner) RunWithStdin(string, string, ...string) error { return nil }
func (p *portFakeRunner) SudoRun(...string) error                      { return nil }
func (p *portFakeRunner) SudoRunQuiet(...string) ([]byte, error)       { return nil, nil }
func (p *portFakeRunner) SudoWrite(string, string) error               { return nil }
func (p *portFakeRunner) SudoMkdir(string) error                       { return nil }
func (p *portFakeRunner) SudoRemove(string) error                      { return nil }
func (p *portFakeRunner) SudoSystemctl(string, string) error           { return nil }
func (p *portFakeRunner) Exists(string) bool                           { return false }
func (p *portFakeRunner) IdentifyPortProcess(string) string            { return "" }

func TestIsRunningFalse(t *testing.T) {
	// docker.IsContainerRunning needs Docker daemon; with empty SDK, returns false.
	_ = docker.SwapNewClient
	// Just verify call doesn't panic; result depends on Docker availability.
	_ = IsRunning()
}

func TestIsDNSRunningFalse(t *testing.T) {
	_ = IsDNSRunning()
}

func TestRestartTraefik(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	config.ResetCache()
	t.Cleanup(config.ResetCache)
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	if err := RestartTraefik(); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRecreateTraefik(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	config.ResetCache()
	t.Cleanup(config.ResetCache)
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	if err := RecreateTraefik(); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestResetRemovesConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	config.ResetCache()
	t.Cleanup(config.ResetCache)
	// Pre-populate a file.
	if err := os.MkdirAll(filepath.Join(root, "traefik"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "x"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	if err := Reset(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("root should be removed, stat err: %v", err)
	}
}
