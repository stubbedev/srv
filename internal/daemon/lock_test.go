package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/srv/internal/config"
)

func lockTestConfig(t *testing.T) *config.Config {
	t.Helper()
	root := setupSrvRoot(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.Root != root {
		t.Fatalf("config root %q, want %q", cfg.Root, root)
	}
	return cfg
}

func TestAcquireDaemonLockExclusive(t *testing.T) {
	cfg := lockTestConfig(t)
	l, err := acquireDaemonLock(cfg)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	_, err = acquireDaemonLock(cfg)
	if err == nil {
		t.Fatal("second acquire succeeded, want refusal")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("refusal %q does not explain the running daemon", err)
	}

	if err := l.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	l2, err := acquireDaemonLock(cfg)
	if err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
	_ = l2.Release()
}

func TestAcquireDaemonLockWritesPid(t *testing.T) {
	cfg := lockTestConfig(t)
	l, err := acquireDaemonLock(cfg)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	t.Cleanup(func() { _ = l.Release() })

	b, err := os.ReadFile(filepath.Join(cfg.Root, LockFile))
	if err != nil {
		t.Fatalf("read lock file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("lock file %q: %v", b, err)
	}
	if pid != os.Getpid() {
		t.Errorf("lock file pid %d, want %d", pid, os.Getpid())
	}
}

func TestServingPid(t *testing.T) {
	cfg := lockTestConfig(t)
	if _, held := ServingPid(cfg); held {
		t.Fatal("lock reported held with no daemon running")
	}

	l, err := acquireDaemonLock(cfg)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	pid, held := ServingPid(cfg)
	if !held {
		t.Fatal("lock reported free while acquired")
	}
	if pid != os.Getpid() {
		t.Errorf("holder pid %d, want %d", pid, os.Getpid())
	}

	_ = l.Release()
	if _, held := ServingPid(cfg); held {
		t.Fatal("lock still reported held after release")
	}
}

func TestDaemonRunRefusesSecondInstance(t *testing.T) {
	d1, err := newDaemonForTest(t)
	if err != nil {
		t.Fatal(err)
	}
	prev := isDockerAvailable
	isDockerAvailable = func() bool { return false }
	t.Cleanup(func() { isDockerAvailable = prev })
	d1.WatchMetadata = false

	runDone := make(chan error, 1)
	go func() { runDone <- d1.Run() }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, held := ServingPid(d1.cfg); held {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first daemon never took the single-instance lock")
		}
		time.Sleep(10 * time.Millisecond)
	}

	d2, err := New()
	if err != nil {
		t.Fatal(err)
	}
	err = d2.Run()
	if err == nil {
		t.Fatal("second daemon Run succeeded, want refusal")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("refusal %q does not explain the running daemon", err)
	}

	d1.cancel()
	select {
	case err := <-runDone:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("first daemon Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("first daemon did not stop after cancel")
	}

	if _, held := ServingPid(d1.cfg); held {
		t.Error("lock still held after the daemon stopped")
	}
}

func TestDaemonLockRefusalNamesPid(t *testing.T) {
	cfg := lockTestConfig(t)
	l, err := acquireDaemonLock(cfg)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	t.Cleanup(func() { _ = l.Release() })

	_, err = acquireDaemonLock(cfg)
	if err == nil {
		t.Fatal("second acquire succeeded")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("pid %d", os.Getpid())) {
		t.Errorf("refusal %q does not name the holder pid", err)
	}
}
