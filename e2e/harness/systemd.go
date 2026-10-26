//go:build e2e

package harness

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	systemdImage = "srv-e2e-systemd"
	// systemdUser is the lingering uid-1000 user e2e/systemd/Dockerfile
	// creates; its user manager is what `systemctl --user` reaches.
	systemdUser       = "dev"
	systemdRuntimeDir = "/run/user/1000"
	// SystemdUserUnitDir is where srv and brew services write user units for
	// systemdUser inside the host.
	SystemdUserUnitDir = "/home/dev/.config/systemd/user"
)

var (
	systemdImageOnce sync.Once
	errSystemdImage  error
)

// SystemdHost is a throwaway container with systemd as PID 1. Suites that
// install, start or remove service units run srv there, so a real
// `systemctl --user` is exercised without touching the machine running the
// tests.
type SystemdHost struct {
	t  *testing.T
	id string
}

// NewSystemdHost boots a SystemdHost with the srv binary under test at
// /usr/local/bin/srv and waits for the user manager to come up. The
// container is removed when the test ends.
func NewSystemdHost(t *testing.T) *SystemdHost {
	t.Helper()
	SkipIfNoEngine(t)
	eng := Engine()

	systemdImageOnce.Do(func() {
		_, file, _, _ := runtime.Caller(0)
		dir := filepath.Join(filepath.Dir(file), "..", "systemd")
		// Built once for every test of the run, so no single test's context.
		out, err := exec.CommandContext(context.Background(), eng.Binary, "build", "--quiet", "--tag", systemdImage, dir).CombinedOutput()
		if err != nil {
			errSystemdImage = fmt.Errorf("%s build %s: %w\n%s", eng.Binary, dir, err, out)
		}
	})
	if errSystemdImage != nil {
		t.Fatal(errSystemdImage)
	}

	out, err := exec.CommandContext(t.Context(), eng.Binary, "run", "--detach",
		"--privileged", "--cgroupns=private", "--tmpfs", "/run", "--tmpfs", "/run/lock",
		systemdImage).Output()
	if err != nil {
		t.Fatalf("start systemd host: %v", err)
	}
	h := &SystemdHost{t: t, id: strings.TrimSpace(string(out))}
	t.Cleanup(func() {
		rm := exec.CommandContext(context.WithoutCancel(t.Context()), eng.Binary, "rm", "--force", h.id)
		if out, err := rm.CombinedOutput(); err != nil {
			t.Logf("remove systemd host %s: %v\n%s", h.id, err, out)
		}
	})

	if out, err := exec.CommandContext(t.Context(), eng.Binary, "cp", BuildSrv(t), h.id+":/usr/local/bin/srv").CombinedOutput(); err != nil {
		t.Fatalf("copy srv into the systemd host: %v\n%s", err, out)
	}
	h.WaitFor("the user manager to start", 60*time.Second, func() bool {
		out, _ := h.exec("root", "systemctl", "is-active", "user@1000.service")
		return strings.TrimSpace(out) == "active"
	})
	return h
}

// Run runs args as the unprivileged user and returns combined output and
// the exit error, for callers that expect a command to fail.
func (h *SystemdHost) Run(args ...string) (string, error) {
	h.t.Helper()
	return h.exec(systemdUser, args...)
}

// MustRun is Run that fails the test on a non-zero exit.
func (h *SystemdHost) MustRun(args ...string) string {
	h.t.Helper()
	out, err := h.Run(args...)
	if err != nil {
		h.t.Fatalf("%v failed in the systemd host: %v\n%s", args, err, out)
	}
	return out
}

// WriteFile writes content to path as the unprivileged user, creating its
// directory.
func (h *SystemdHost) WriteFile(path, content string) {
	h.t.Helper()
	h.MustRun("sh", "-c", `mkdir -p "$(dirname "$1")" && printf %s "$2" > "$1"`, "sh", path, content)
}

// Exists reports whether path exists in the host.
func (h *SystemdHost) Exists(path string) bool {
	h.t.Helper()
	_, err := h.Run("test", "-e", path)
	return err == nil
}

// UnitState returns `systemctl --user is-active` for unit.
func (h *SystemdHost) UnitState(unit string) string {
	h.t.Helper()
	out, _ := h.Run("systemctl", "--user", "is-active", unit)
	return strings.TrimSpace(out)
}

// UnitMainPID returns the main PID systemd reports for unit, "0" when none.
func (h *SystemdHost) UnitMainPID(unit string) string {
	h.t.Helper()
	return strings.TrimSpace(h.MustRun("systemctl", "--user", "show", "--property", "MainPID", "--value", unit))
}

// WaitFor polls cond every 250ms until it holds, failing the test with what
// after timeout.
func (h *SystemdHost) WaitFor(what string, timeout time.Duration, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out after %s waiting for %s", timeout, what)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// exec runs args in the host as user. The user's runtime dir and session
// bus are set so `systemctl --user` reaches its user manager.
func (h *SystemdHost) exec(user string, args ...string) (string, error) {
	cmdArgs := []string{
		"exec", "--user", user,
		"--env", "XDG_RUNTIME_DIR=" + systemdRuntimeDir,
		"--env", "DBUS_SESSION_BUS_ADDRESS=unix:path=" + systemdRuntimeDir + "/bus",
		h.id,
	}
	out, err := exec.CommandContext(h.t.Context(), Engine().Binary, append(cmdArgs, args...)...).CombinedOutput()
	return string(out), err
}
