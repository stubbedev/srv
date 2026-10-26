//go:build e2e

// End-to-end coverage for the daemon's service unit against a real
// `systemctl --user`, inside a disposable systemd container: srv's own unit,
// the unit `brew services start srv` writes, and both at once. The bug this
// suite exists for: `srv install` added srv's unit next to the brew one and
// the two daemons raced.
package daemonservice_test

import (
	"path"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/srv/e2e/harness"
	"github.com/stubbedev/srv/internal/daemon"
)

var (
	srvUnitPath  = path.Join(harness.SystemdUserUnitDir, daemon.SystemdServiceName)
	brewUnitPath = path.Join(harness.SystemdUserUnitDir, daemon.BrewSystemdServiceName)
)

// installBrewUnit writes and starts the unit `brew services start srv`
// generates on Linux from the formula's service block. It does not wait for
// the daemon: next to srv's unit it loses the single-instance lock and fails.
func installBrewUnit(h *harness.SystemdHost) {
	h.WriteFile(brewUnitPath, `[Unit]
Description=Homebrew generated unit for srv

[Install]
WantedBy=default.target

[Service]
Type=simple
ExecStart=/usr/local/bin/srv daemon start --foreground
Restart=on-failure
`)
	h.MustRun("systemctl", "--user", "daemon-reload")
	h.MustRun("systemctl", "--user", "enable", "--now", daemon.BrewSystemdServiceName)
}

func waitState(h *harness.SystemdHost, unit, want string) {
	h.WaitFor(unit+" to be "+want, 30*time.Second, func() bool { return h.UnitState(unit) == want })
}

func mustContain(t *testing.T, out, want string) {
	t.Helper()
	if !strings.Contains(out, want) {
		t.Fatalf("output does not contain %q:\n%s", want, out)
	}
}

func TestSrvUnitLifecycle(t *testing.T) {
	h := harness.NewSystemdHost(t)

	h.MustRun("srv", "daemon", "install")
	if !h.Exists(srvUnitPath) {
		t.Fatalf("srv daemon install wrote no %s", srvUnitPath)
	}
	waitState(h, daemon.SystemdServiceName, "active")
	mustContain(t, h.MustRun("srv", "daemon", "status"), "installed via srv (active)")

	h.MustRun("srv", "daemon", "stop")
	waitState(h, daemon.SystemdServiceName, "inactive")
	h.MustRun("srv", "daemon", "start")
	waitState(h, daemon.SystemdServiceName, "active")

	h.MustRun("srv", "daemon", "uninstall")
	if h.Exists(srvUnitPath) {
		t.Errorf("srv daemon uninstall left %s behind", srvUnitPath)
	}
	if state := h.UnitState(daemon.SystemdServiceName); state == "active" {
		t.Errorf("%s still active after uninstall", daemon.SystemdServiceName)
	}
	mustContain(t, h.MustRun("srv", "daemon", "status"), "Service:  not installed")
}

func TestBrewServicesUnitIsDrivenNotDuplicated(t *testing.T) {
	h := harness.NewSystemdHost(t)
	installBrewUnit(h)
	waitState(h, daemon.BrewSystemdServiceName, "active")

	mustContain(t, h.MustRun("srv", "daemon", "install"), "already runs under 'brew services'")
	if h.Exists(srvUnitPath) {
		t.Fatalf("srv daemon install wrote %s next to the brew services unit", srvUnitPath)
	}
	mustContain(t, h.MustRun("srv", "daemon", "status"), "installed via brew services (active)")

	before := h.UnitMainPID(daemon.BrewSystemdServiceName)
	h.MustRun("srv", "daemon", "restart")
	h.WaitFor("the brew services daemon to restart", 30*time.Second, func() bool {
		pid := h.UnitMainPID(daemon.BrewSystemdServiceName)
		return pid != "0" && pid != before
	})

	h.MustRun("srv", "daemon", "stop")
	waitState(h, daemon.BrewSystemdServiceName, "inactive")

	out, err := h.Run("srv", "daemon", "uninstall")
	if err == nil {
		t.Fatalf("srv daemon uninstall removed a unit brew services owns:\n%s", out)
	}
	mustContain(t, out, "brew services stop srv")
	if !h.Exists(brewUnitPath) {
		t.Errorf("srv daemon uninstall deleted the brew services unit")
	}
}

func TestDoubleInstallIsReportedAndResolved(t *testing.T) {
	h := harness.NewSystemdHost(t)
	h.MustRun("srv", "daemon", "install")
	installBrewUnit(h)

	mustContain(t, h.MustRun("srv", "daemon", "status"), "installed twice")
	out, err := h.Run("srv", "daemon", "restart")
	if err == nil {
		t.Fatalf("srv daemon restart picked one of two units:\n%s", out)
	}
	mustContain(t, out, "installed twice")

	h.MustRun("srv", "daemon", "uninstall")
	if h.Exists(srvUnitPath) {
		t.Fatalf("srv daemon uninstall left %s behind", srvUnitPath)
	}
	if !h.Exists(brewUnitPath) {
		t.Fatalf("srv daemon uninstall deleted the brew services unit")
	}
	waitState(h, daemon.BrewSystemdServiceName, "active")
	mustContain(t, h.MustRun("srv", "daemon", "status"), "installed via brew services (active)")
}
