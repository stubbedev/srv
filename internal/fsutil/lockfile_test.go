package fsutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Two goroutines in one process must serialize, never interleave.
func TestWithFileLockSerializesGoroutines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guard.lock")
	var inUse, peak atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := WithFileLock(path, func() error {
				cur := inUse.Add(1)
				for {
					old := peak.Load()
					if cur <= old || peak.CompareAndSwap(old, cur) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				inUse.Add(-1)
				return nil
			})
			if err != nil {
				t.Errorf("WithFileLock: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := peak.Load(); got != 1 {
		t.Errorf("peak concurrency under the lock = %d, want 1", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("lock file %s left behind after release (stat err = %v)", path, err)
	}
}

// The lock guards across processes: while the helper child holds it, the
// parent's acquire must wait. Re-executing the test binary is the standard
// subprocess-fixture pattern.
func TestWithFileLockCrossProcess(t *testing.T) {
	const helperEnv = "SRV_TEST_LOCK_HELPER"
	if os.Getenv(helperEnv) == "1" {
		err := WithFileLock(os.Getenv("SRV_TEST_LOCK_PATH"), func() error {
			time.Sleep(400 * time.Millisecond)
			return nil
		})
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}

	path := filepath.Join(t.TempDir(), "cross.lock")
	cmd := exec.Command(os.Args[0], "-test.run", "TestWithFileLockCrossProcess")
	cmd.Env = append(os.Environ(), helperEnv+"=1", "SRV_TEST_LOCK_PATH="+path)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Wait() }()

	// Wait for the child to actually hold the lock before timing the parent.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	start := time.Now()
	if err := WithFileLock(path, func() error { return nil }); err != nil {
		t.Fatalf("WithFileLock: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Errorf("lock acquired after %v while another process held it", elapsed)
	}
}

// A holder that crashed without removing its lock file must not block
// forever: an old lock file is broken by the next acquirer.
func TestWithFileLockBreaksStaleLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stale.lock")
	if err := os.WriteFile(path, []byte("99999"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * lockStaleAge)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- WithFileLock(path, func() error { return nil })
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("stale lock not broken: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stale lock blocked acquisition")
	}
}
