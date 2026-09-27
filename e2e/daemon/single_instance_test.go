//go:build e2e

// End-to-end coverage of the single-instance rule: whatever else is running,
// exactly one srv daemon may exist per srv root, and it alone serves the
// embedded DNS (127.0.0.1:15353) and static file server (127.0.0.1:15380)
// for every site and container. A second daemon start refuses on the lock,
// standalone `srv dnsd` / `srv httpd` name the daemon as the port holder,
// and both ports free up the moment the daemon stops. Needs no container
// engine: the daemon keeps running without one.
package daemon_test

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stubbedev/srv/e2e/harness"
	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/daemon"
)

func TestSingleDaemonInstance(t *testing.T) {
	harness.SkipIfPortsBusy(t)
	root := harness.NewRoot(t)
	bin := harness.BuildSrv(t)
	srvEnv := append(os.Environ(), constants.EnvSrvRoot+"="+root)

	var daemonOut bytes.Buffer
	d := exec.Command(bin, "daemon", "start", "--foreground", "--no-watch")
	d.Env = srvEnv
	d.Stdout = &daemonOut
	d.Stderr = &daemonOut
	if err := d.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	daemonStopped := false
	t.Cleanup(func() {
		if daemonStopped {
			return
		}
		_ = d.Process.Signal(syscall.SIGTERM)
		_, _ = d.Process.Wait()
	})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	waitFor(t, 15*time.Second, "daemon lock", func() bool {
		_, held := daemon.ServingPid(cfg)
		return held
	})
	lockPid := readLockPid(t, filepath.Join(root, daemon.LockFile))
	if lockPid != d.Process.Pid {
		t.Errorf("lock names pid %d, want daemon pid %d", lockPid, d.Process.Pid)
	}

	out, err := harness.RunSrvAllowErr(t, root, "daemon", "start", "--foreground", "--no-watch")
	if err == nil {
		t.Fatalf("second daemon start succeeded:\n%s", out)
	}
	if !strings.Contains(out, "already running") {
		t.Errorf("second start output %q does not name the running daemon", out)
	}

	out, err = harness.RunSrvAllowErr(t, root, "dnsd")
	if err == nil {
		t.Fatalf("standalone dnsd succeeded beside the daemon:\n%s", out)
	}
	if !strings.Contains(out, "srv daemon") {
		t.Errorf("dnsd output %q does not name the daemon as the holder", out)
	}

	out, err = harness.RunSrvAllowErr(t, root, "httpd")
	if err == nil {
		t.Fatalf("standalone httpd succeeded beside the daemon:\n%s", out)
	}
	if !strings.Contains(out, "srv daemon") {
		t.Errorf("httpd output %q does not name the daemon as the holder", out)
	}

	_ = d.Process.Signal(syscall.SIGTERM)
	daemonWait := make(chan struct{})
	go func() { _ = d.Wait(); close(daemonWait) }()
	select {
	case <-daemonWait:
		daemonStopped = true
	case <-time.After(15 * time.Second):
		_ = d.Process.Kill()
		<-daemonWait
		daemonStopped = true
		t.Logf("daemon needed SIGKILL to stop; output:\n%s", daemonOut.String())
	}
	waitFor(t, 5*time.Second, "lock release", func() bool {
		_, held := daemon.ServingPid(cfg)
		return !held
	})

	var httpdOut bytes.Buffer
	h := exec.Command(bin, "httpd")
	h.Env = srvEnv
	h.Stdout = &httpdOut
	h.Stderr = &httpdOut
	if err := h.Start(); err != nil {
		t.Fatalf("standalone httpd after daemon stop: %v", err)
	}
	t.Cleanup(func() {
		_ = h.Process.Signal(syscall.SIGTERM)
		_, _ = h.Process.Wait()
	})
	waitFor(t, 10*time.Second, "static port handover", func() bool {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(constants.LocalhostIP, constants.PortStaticStr), 250*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	})
}

func waitFor(t *testing.T, timeout time.Duration, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen within %s", what, timeout)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func readLockPid(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read lock file: %v", err)
	}
	pid := 0
	if _, err := fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &pid); err != nil {
		t.Fatalf("lock file %q: %v", b, err)
	}
	return pid
}
