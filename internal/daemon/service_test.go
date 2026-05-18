package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/platform"
	"github.com/stubbedev/srv/internal/shell"
	"github.com/stubbedev/srv/internal/shell/shelltest"
)

func swapShell(t *testing.T, r shell.Runner) {
	t.Helper()
	t.Cleanup(shell.SwapDefault(r))
}

func TestSystemdServicePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path, err := systemdServicePath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, SystemdUserDir, SystemdServiceName)
	if path != want {
		t.Errorf("got %q, want %q", path, want)
	}
}

func TestLaunchdPlistPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path, err := launchdPlistPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, LaunchAgentsDir, LaunchdPlistName)
	if path != want {
		t.Errorf("got %q, want %q", path, want)
	}
}

func TestIsSystemdInstalledMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if isSystemdInstalled() {
		t.Error("expected false")
	}
}

func TestIsSystemdInstalledPresent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	servicePath, _ := systemdServicePath()
	if err := os.MkdirAll(filepath.Dir(servicePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(servicePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isSystemdInstalled() {
		t.Error("expected true")
	}
}

func TestIsLaunchdInstalledMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if isLaunchdInstalled() {
		t.Error("expected false")
	}
}

func TestServicePathLinux(t *testing.T) {
	if !platform.IsLinux() {
		t.Skip("Linux-only path")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	got, err := ServicePath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, SystemdUserDir, SystemdServiceName)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestServiceStatusLinuxActive(t *testing.T) {
	if !platform.IsLinux() {
		t.Skip("Linux-only path")
	}
	swapShell(t, shelltest.New(map[string]shelltest.Response{
		"systemctl": {Out: []byte("active\n")},
	}))
	status, err := ServiceStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status != "active" {
		t.Errorf("got %q", status)
	}
}

func TestServiceStatusLinuxInactive(t *testing.T) {
	if !platform.IsLinux() {
		t.Skip("Linux-only path")
	}
	swapShell(t, shelltest.New(map[string]shelltest.Response{
		"systemctl": {Err: errors.New("exit code 3")},
	}))
	status, err := ServiceStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status != "inactive" {
		t.Errorf("got %q", status)
	}
}

func TestRestartLinux(t *testing.T) {
	if !platform.IsLinux() {
		t.Skip("Linux-only")
	}
	swapShell(t, shelltest.New(nil))
	if err := Restart(); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRestartLinuxErr(t *testing.T) {
	if !platform.IsLinux() {
		t.Skip("Linux-only")
	}
	swapShell(t, shelltest.New(map[string]shelltest.Response{
		"systemctl": {Err: errors.New("fail")},
	}))
	if err := Restart(); err == nil {
		t.Error("expected err")
	}
}

func TestUninstallLinuxMissingFile(t *testing.T) {
	if !platform.IsLinux() {
		t.Skip("Linux-only")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	swapShell(t, shelltest.New(nil))
	// Missing service file should not error.
	if err := Uninstall(); err != nil {
		t.Errorf("Uninstall missing -> %v", err)
	}
}

func TestUninstallLinuxRemovesFile(t *testing.T) {
	if !platform.IsLinux() {
		t.Skip("Linux-only")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	servicePath, _ := systemdServicePath()
	if err := os.MkdirAll(filepath.Dir(servicePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(servicePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	swapShell(t, shelltest.New(nil))
	if err := Uninstall(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(servicePath); !os.IsNotExist(err) {
		t.Errorf("service file should be removed: %v", err)
	}
}

func TestInstallLinux(t *testing.T) {
	if !platform.IsLinux() {
		t.Skip("Linux-only")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	swapShell(t, shelltest.New(nil))
	if err := Install(); err != nil {
		t.Fatalf("Install err: %v", err)
	}
	servicePath, _ := systemdServicePath()
	data, err := os.ReadFile(servicePath)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(data), "srv daemon") {
		t.Errorf("service file content unexpected: %q", string(data))
	}
}

func TestInstallLinuxReloadErr(t *testing.T) {
	if !platform.IsLinux() {
		t.Skip("Linux-only")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	swapShell(t, shelltest.New(map[string]shelltest.Response{
		"systemctl": {Err: errors.New("fail")},
	}))
	if err := Install(); err == nil {
		t.Error("expected daemon-reload err to propagate")
	}
}

func TestInstallLinuxEnableErr(t *testing.T) {
	if !platform.IsLinux() {
		t.Skip("Linux-only")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	calls := 0
	swapShell(t, &errAfterShell{n: 2, calls: &calls})
	if err := Install(); err == nil {
		t.Error("expected enable err")
	}
}

func TestInstallLinuxStartErr(t *testing.T) {
	if !platform.IsLinux() {
		t.Skip("Linux-only")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	calls := 0
	swapShell(t, &errAfterShell{n: 3, calls: &calls})
	if err := Install(); err == nil {
		t.Error("expected start err")
	}
}

func TestIsInstalledFallsThroughOnUnknownOS(t *testing.T) {
	// Can't change runtime.GOOS. Just exercise the branch we're on.
	got := IsInstalled()
	_ = got
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		// On other OSes IsInstalled should return false.
		if got {
			t.Errorf("expected false on %s, got true", runtime.GOOS)
		}
	}
}

func TestStopServiceLinux(t *testing.T) {
	if !platform.IsLinux() {
		t.Skip("linux only")
	}
	swapShell(t, shelltest.New(nil))
	if err := stopService(); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestServicePathLinuxResolves(t *testing.T) {
	if !platform.IsLinux() {
		t.Skip("linux only")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	_, err := ServicePath()
	if err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestServiceStatusUnsupported(t *testing.T) {
	// Just exercise on this platform; result depends on host.
	status, _ := ServiceStatus()
	_ = status
}

// Launchd helpers don't gate on runtime.GOOS internally — we can invoke them
// directly to exercise the file-writing paths on any host.
func TestInstallLaunchdWritesPlist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	config.ResetCache()
	t.Cleanup(config.ResetCache)
	swapShell(t, shelltest.New(nil))
	if err := installLaunchd(); err != nil {
		t.Errorf("err: %v", err)
	}
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "dev.stubbe.srv-daemon.plist")
	if _, err := os.Stat(plistPath); err != nil {
		t.Errorf("plist missing: %v", err)
	}
}

func TestUninstallLaunchd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "dev.stubbe.srv-daemon.plist")
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plistPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	swapShell(t, shelltest.New(nil))
	if err := uninstallLaunchd(); err != nil {
		t.Errorf("err: %v", err)
	}
	if _, err := os.Stat(plistPath); !os.IsNotExist(err) {
		t.Errorf("plist should be gone: %v", err)
	}
}

func TestRestartLaunchd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	swapShell(t, shelltest.New(nil))
	if err := restartLaunchd(); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestGetLaunchdStatusNotLoaded(t *testing.T) {
	swapShell(t, shelltest.New(map[string]shelltest.Response{
		"launchctl": {Err: errors.New("not loaded")},
	}))
	status, err := GetLaunchdStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status != "not loaded" {
		t.Errorf("got %q", status)
	}
}

func TestGetLaunchdStatusRunning(t *testing.T) {
	swapShell(t, shelltest.New(map[string]shelltest.Response{
		"launchctl": {Out: []byte(`{"PID" = 1234;}`)},
	}))
	status, err := GetLaunchdStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status != "running" {
		t.Errorf("got %q", status)
	}
}

func TestGetLaunchdStatusLoaded(t *testing.T) {
	swapShell(t, shelltest.New(map[string]shelltest.Response{
		"launchctl": {Out: []byte(`{"Label" = "x";}`)},
	}))
	status, err := GetLaunchdStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status != "loaded" {
		t.Errorf("got %q", status)
	}
}

// errAfterShell counts Run calls and errors on the Nth call. Other methods
// delegate to a no-op stub.
type errAfterShell struct {
	n     int
	calls *int
}

func (e *errAfterShell) Run(name string, args ...string) error {
	*e.calls++
	if *e.calls >= e.n {
		return errors.New("forced")
	}
	return nil
}
func (e *errAfterShell) RunWithContext(_ context.Context, _ string, _ ...string) error { return nil }
func (e *errAfterShell) RunQuiet(string, ...string) ([]byte, error)                    { return nil, nil }
func (e *errAfterShell) RunQuietWithContext(_ context.Context, _ string, _ ...string) ([]byte, error) {
	return nil, nil
}
func (e *errAfterShell) RunWithStdin(string, string, ...string) error { return nil }
func (e *errAfterShell) SudoRun(...string) error                      { return nil }
func (e *errAfterShell) SudoRunQuiet(...string) ([]byte, error)       { return nil, nil }
func (e *errAfterShell) SudoWrite(string, string) error               { return nil }
func (e *errAfterShell) SudoMkdir(string) error                       { return nil }
func (e *errAfterShell) SudoRemove(string) error                      { return nil }
func (e *errAfterShell) SudoSystemctl(string, string) error           { return nil }
func (e *errAfterShell) Exists(string) bool                           { return false }
func (e *errAfterShell) CheckPort(string) (bool, error)               { return false, nil }
func (e *errAfterShell) CheckPortOnAddr(string, string) (bool, error) { return false, nil }
func (e *errAfterShell) IdentifyPortProcess(string) string            { return "" }
