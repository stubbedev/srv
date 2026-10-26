package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stubbedev/srv/internal/platform"
	"github.com/stubbedev/srv/internal/shell/shelltest"
)

// tempHome points HOME at a fresh temp dir, where unitFor resolves every
// unit file, and returns it. It skips on platforms without service units.
func tempHome(t *testing.T) string {
	t.Helper()
	if !platform.IsLinux() && !platform.IsDarwin() {
		t.Skip("service units exist only on Linux and macOS")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

// installUnit writes the unit file of supervisor s under HOME, as
// `srv daemon install` or `brew services start srv` would.
func installUnit(t *testing.T, s Supervisor) {
	t.Helper()
	path := unitPath(t, s)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func unitPath(t *testing.T, s Supervisor) string {
	t.Helper()
	u, err := unitFor(s)
	if err != nil {
		t.Fatal(err)
	}
	return u.path
}

func TestActiveSupervisor(t *testing.T) {
	cases := []struct {
		name      string
		installed []Supervisor
		want      Supervisor
		double    bool
	}{
		{name: "nothing installed", want: SupervisorNone},
		{name: "srv unit", installed: []Supervisor{SupervisorSrv}, want: SupervisorSrv},
		{name: "brew services unit", installed: []Supervisor{SupervisorBrew}, want: SupervisorBrew},
		{name: "both", installed: []Supervisor{SupervisorSrv, SupervisorBrew}, double: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tempHome(t)
			for _, s := range c.installed {
				installUnit(t, s)
			}
			got, err := ActiveSupervisor()
			var dbl *DoubleSupervisedError
			if c.double {
				if !errors.As(err, &dbl) {
					t.Fatalf("err = %v, want *DoubleSupervisedError", err)
				}
				if dbl.SrvUnit != unitPath(t, SupervisorSrv) || dbl.BrewUnit != unitPath(t, SupervisorBrew) {
					t.Errorf("error names %q and %q, want both unit files", dbl.SrvUnit, dbl.BrewUnit)
				}
				if !IsInstalled() {
					t.Error("IsInstalled() = false with two units installed")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("ActiveSupervisor() = %v, want %v", got, c.want)
			}
			if IsInstalled() != (c.want != SupervisorNone) {
				t.Errorf("IsInstalled() = %v with %v", IsInstalled(), c.want)
			}
		})
	}
}

// The bug this guards: `srv install` installed srv's own unit next to the
// one `brew services start srv` had written, and the two raced.
func TestInstallRefusesWhenBrewServicesSupervises(t *testing.T) {
	tempHome(t)
	installUnit(t, SupervisorBrew)
	fake := shelltest.New(nil)
	swapShell(t, fake)

	err := Install()
	if !errors.Is(err, ErrSupervisedByBrew) {
		t.Fatalf("Install() = %v, want ErrSupervisedByBrew", err)
	}
	if _, statErr := os.Stat(unitPath(t, SupervisorSrv)); !os.IsNotExist(statErr) {
		t.Errorf("srv unit was written next to the brew services one: %v", statErr)
	}
	if calls := fake.Snapshot(); len(calls) != 0 {
		t.Errorf("Install() ran %v, want no service-manager calls", calls)
	}
}

func TestUninstallLeavesTheBrewServicesUnitAlone(t *testing.T) {
	tempHome(t)
	installUnit(t, SupervisorBrew)
	swapShell(t, shelltest.New(nil))

	if err := Uninstall(); !errors.Is(err, ErrSupervisedByBrew) {
		t.Fatalf("Uninstall() = %v, want ErrSupervisedByBrew", err)
	}
	if _, err := os.Stat(unitPath(t, SupervisorBrew)); err != nil {
		t.Errorf("brew services unit was touched: %v", err)
	}
}

func TestUninstallResolvesADoubleInstall(t *testing.T) {
	tempHome(t)
	installUnit(t, SupervisorSrv)
	installUnit(t, SupervisorBrew)
	swapShell(t, shelltest.New(nil))

	if err := Uninstall(); err != nil {
		t.Fatal(err)
	}
	got, err := ActiveSupervisor()
	if err != nil || got != SupervisorBrew {
		t.Errorf("after Uninstall: ActiveSupervisor() = %v, %v; want brew services, nil", got, err)
	}
}

// Lifecycle commands must drive the unit that actually runs the daemon.
func TestRestartAndStatusRouteToTheBrewServicesUnit(t *testing.T) {
	tempHome(t)
	installUnit(t, SupervisorBrew)
	fake := shelltest.New(map[string]shelltest.Response{
		"systemctl": {Out: []byte("active\n")},
		"launchctl": {Out: []byte(`{"PID" = 1;}`)},
	})
	swapShell(t, fake)
	brew, err := unitFor(SupervisorBrew)
	if err != nil {
		t.Fatal(err)
	}

	if err := Restart(); err != nil {
		t.Fatal(err)
	}
	if _, err := ServiceStatus(); err != nil {
		t.Fatal(err)
	}
	if path, err := ServicePath(); err != nil || path != brew.path {
		t.Errorf("ServicePath() = %q, %v; want %q", path, err, brew.path)
	}
	for _, c := range fake.Snapshot() {
		if !slices.Contains(c.Args, brew.name) && !slices.Contains(c.Args, brew.path) {
			t.Errorf("%s %v does not target the brew services unit %s", c.Name, c.Args, brew.name)
		}
	}
}

func TestServiceStatusReportsADoubleInstall(t *testing.T) {
	tempHome(t)
	installUnit(t, SupervisorSrv)
	installUnit(t, SupervisorBrew)
	swapShell(t, shelltest.New(nil))

	var dbl *DoubleSupervisedError
	if _, err := ServiceStatus(); !errors.As(err, &dbl) {
		t.Errorf("ServiceStatus() err = %v, want *DoubleSupervisedError", err)
	}
	if err := Restart(); !errors.As(err, &dbl) {
		t.Errorf("Restart() err = %v, want *DoubleSupervisedError", err)
	}
}

func TestServiceStatusWithoutAUnit(t *testing.T) {
	tempHome(t)
	status, err := ServiceStatus()
	if err != nil || status != "not installed" {
		t.Errorf("ServiceStatus() = %q, %v; want \"not installed\", nil", status, err)
	}
}
