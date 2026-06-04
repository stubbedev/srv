package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	dockerevents "github.com/docker/docker/api/types/events"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/site"
)

func setupSrvRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	config.ResetCache()
	t.Cleanup(config.ResetCache)
	return root
}

func TestLogPath(t *testing.T) {
	cfg := &config.Config{Root: "/srv"}
	if got := LogPath(cfg); got != filepath.Join("/srv", LogFile) {
		t.Errorf("got %q", got)
	}
}

func TestNewDaemon(t *testing.T) {
	setupSrvRoot(t)
	d, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if d == nil {
		t.Fatal("nil daemon")
	}
	if d.networkName == "" {
		t.Error("networkName empty")
	}
	if !d.WatchMetadata {
		t.Error("WatchMetadata should default true")
	}
	if d.containers == nil {
		t.Error("containers map nil")
	}
}

func TestDaemonLogWritesTimestamped(t *testing.T) {
	root := setupSrvRoot(t)
	d := &Daemon{cfg: &config.Config{Root: root}}
	logPath := filepath.Join(root, "test.log")
	f, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d.logFile = f
	d.log("hello %s", "world")
	data, _ := os.ReadFile(logPath)
	body := string(data)
	if !contains(body, "hello world") {
		t.Errorf("log missing message: %q", body)
	}
}

func TestDaemonLogNilFileNoCrash(t *testing.T) {
	d := &Daemon{}
	d.log("safe %d", 1)
}

// TestDaemonLogConcurrent confirms log() is safe under concurrent callers
// (signal, metadata-watcher, and event goroutines all call it). Run with
// `go test -race` this fails without the logMu guard; it also asserts every
// line lands intact (no interleaving/truncation).
func TestDaemonLogConcurrent(t *testing.T) {
	root := setupSrvRoot(t)
	d := &Daemon{cfg: &config.Config{Root: root}}
	logPath := filepath.Join(root, "concurrent.log")
	f, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d.logFile = f

	const goroutines, perGoroutine = 8, 50
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				d.log("g%d-line%d", id, i)
			}
		}(g)
	}
	wg.Wait()

	data, _ := os.ReadFile(logPath)
	lines := strings.Count(string(data), "\n")
	if want := goroutines * perGoroutine; lines != want {
		t.Errorf("got %d log lines, want %d (interleaved/lost writes)", lines, want)
	}
}

func TestRefreshContainerMappingNoSites(t *testing.T) {
	setupSrvRoot(t)
	d, err := New()
	if err != nil {
		t.Fatal(err)
	}
	d.containers["existing"] = "site"
	if err := d.refreshContainerMapping(); err != nil {
		t.Fatal(err)
	}
	if len(d.containers) != 0 {
		t.Errorf("expected empty map, got %v", d.containers)
	}
}

func TestHandleContainerStartUntrackedNoop(t *testing.T) {
	root := setupSrvRoot(t)
	d := &Daemon{
		cfg:         &config.Config{Root: root},
		networkName: "n",
		containers:  map[string]string{},
	}
	logPath := filepath.Join(root, "x.log")
	f, _ := os.Create(logPath)
	defer f.Close()
	d.logFile = f
	d.lastRefreshTime = time.Now() // suppress refresh attempt
	d.handleContainerStart(dockerevents.Message{
		Actor: dockerevents.Actor{Attributes: map[string]string{"name": "ghost"}},
	})
	// Should produce no log line.
	data, _ := os.ReadFile(logPath)
	if len(data) > 0 {
		t.Errorf("unexpected log: %q", string(data))
	}
}

func TestHandleContainerStartNoName(t *testing.T) {
	d := &Daemon{
		cfg:         &config.Config{Root: t.TempDir()},
		networkName: "n",
		containers:  map[string]string{},
	}
	d.handleContainerStart(dockerevents.Message{}) // no name attribute
}

func TestIsDirectChild(t *testing.T) {
	if !isDirectChild("/srv", "/srv/blog") {
		t.Error("expected direct child")
	}
	if isDirectChild("/srv", "/srv/blog/sub") {
		t.Error("nested should not count")
	}
}

func TestReadDirSafeMissing(t *testing.T) {
	entries, err := readDirSafe("/nonexistent-dir-srv-daemon")
	if err != nil {
		t.Fatal(err)
	}
	if entries != nil {
		t.Errorf("expected nil entries, got %v", entries)
	}
}

func TestReadDirSafeReal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := readDirSafe(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
}

func TestHandleContainerStartTracked(t *testing.T) {
	// We can't run docker.ConnectContainerToNetwork against a real daemon,
	// so swap the SDK newClient seam by using the docker pkg's hook via
	// indirection — but daemon_test can't import docker_test. Skip the
	// network connect path here; just verify the log path runs without
	// panicking when the container is tracked.
	root := setupSrvRoot(t)
	d := &Daemon{
		cfg:         &config.Config{Root: root},
		networkName: "n",
		containers:  map[string]string{"web": "blog"},
	}
	f, _ := os.Create(filepath.Join(root, "x.log"))
	defer f.Close()
	d.logFile = f
	d.lastRefreshTime = time.Now()
	d.handleContainerStart(dockerevents.Message{
		Actor: dockerevents.Actor{Attributes: map[string]string{"name": "web"}},
	})
	data, _ := os.ReadFile(filepath.Join(root, "x.log"))
	if !contains(string(data), "Container web started") {
		t.Errorf("expected start log, got: %q", string(data))
	}
}

func TestStopWhenNotInstalled(t *testing.T) {
	// IsInstalled returns false for a missing service path. Stop should error.
	t.Setenv("HOME", t.TempDir())
	if err := Stop(); err == nil {
		t.Error("expected error when service not installed")
	} else if !contains(err.Error(), "not installed") {
		t.Errorf("err = %v", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestWaitForDockerCancellation(t *testing.T) {
	d, err := newDaemonForTest(t)
	if err != nil {
		t.Fatal(err)
	}
	prev := isDockerAvailable
	isDockerAvailable = func() bool { return false }
	t.Cleanup(func() { isDockerAvailable = prev })
	// Cancel quickly so backoff loop exits.
	go func() {
		time.Sleep(50 * time.Millisecond)
		d.cancel()
	}()
	if err := d.waitForDocker(); err == nil {
		t.Error("expected ctx error after cancel")
	}
}

func TestWaitForDockerAvailable(t *testing.T) {
	d, err := newDaemonForTest(t)
	if err != nil {
		t.Fatal(err)
	}
	prev := isDockerAvailable
	isDockerAvailable = func() bool { return true }
	t.Cleanup(func() { isDockerAvailable = prev })
	if err := d.waitForDocker(); err != nil {
		t.Errorf("unexpected err: %v", err)
	}
}

func TestIsRunning(t *testing.T) {
	// Just exercise; result depends on host.
	_ = IsRunning()
}

func TestDaemonRunCancelEarly(t *testing.T) {
	d, err := newDaemonForTest(t)
	if err != nil {
		t.Fatal(err)
	}
	prev := isDockerAvailable
	isDockerAvailable = func() bool { return false }
	t.Cleanup(func() { isDockerAvailable = prev })

	d.WatchMetadata = true
	go func() {
		time.Sleep(100 * time.Millisecond)
		d.cancel()
	}()
	// Run may return context.Canceled; just exercise the path.
	_ = d.Run()
}

func TestDaemonRunNoWatch(t *testing.T) {
	d, err := newDaemonForTest(t)
	if err != nil {
		t.Fatal(err)
	}
	prev := isDockerAvailable
	isDockerAvailable = func() bool { return false }
	t.Cleanup(func() { isDockerAvailable = prev })

	d.WatchMetadata = false
	go func() {
		time.Sleep(100 * time.Millisecond)
		d.cancel()
	}()
	_ = d.Run()
}

func TestRefreshContainerMappingWithSites(t *testing.T) {
	root := setupSrvRoot(t)
	d, err := New()
	if err != nil {
		t.Fatal(err)
	}
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := site.WriteSiteMetadata("blog", site.SiteMetadata{
		Type:        site.SiteTypeCompose,
		Domains:     []string{"blog.local"},
		ProjectPath: projectDir,
		ServiceName: "blog-web",
		Port:        80,
		NetworkName: "n",
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.refreshContainerMapping(); err != nil {
		t.Fatal(err)
	}
	if d.containers["blog-web"] != "blog" {
		t.Errorf("got %v", d.containers)
	}
}

func newDaemonForTest(t *testing.T) (*Daemon, error) {
	t.Helper()
	setupSrvRoot(t)
	return New()
}
