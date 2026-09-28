package site

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
)

// lifecycleFixture registers one daemon-served site ("page") and one
// container site ("blog"), and records every compose invocation.
func lifecycleFixture(t *testing.T) (engineCalls *atomic.Int64, composeDirs func() []string) {
	t.Helper()
	root := withSRVRoot(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.TraefikConfDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, meta := range map[string]SiteMetadata{
		"page": {Type: SiteTypeStatic, Domains: []string{"page.local"}, ProjectPath: projectDir, DaemonServed: true},
		"blog": {Type: SiteTypeStatic, Domains: []string{"blog.local"}, ProjectPath: projectDir, Port: 80, NetworkName: cfg.NetworkName},
	} {
		if err := WriteSiteMetadata(name, meta); err != nil {
			t.Fatal(err)
		}
	}

	engineCalls = new(atomic.Int64)
	t.Cleanup(docker.SwapNewClientWithNetworkCounted(cfg.NetworkName, engineCalls))
	var mu sync.Mutex
	var dirs []string
	t.Cleanup(docker.SwapComposeExec(func(dir string, _ bool, _ ...string) error {
		mu.Lock()
		defer mu.Unlock()
		dirs = append(dirs, dir)
		return nil
	}))
	return engineCalls, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), dirs...)
	}
}

func mustRequire(t *testing.T, name string) *Site {
	t.Helper()
	s, err := Require(name)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Regression for #12: `srv install` handed daemon-served sites to compose.
// Every Runner operation on a daemon-served site must stay off the engine and
// off compose entirely.
func TestRunnerDaemonServedNeverTouchesDocker(t *testing.T) {
	engineCalls, composeDirs := lifecycleFixture(t)
	var r Runner
	for name, op := range map[string]func(*Site) error{
		"start":   func(s *Site) error { return r.Start(s, false) },
		"apply":   r.Apply,
		"restart": func(s *Site) error { return r.Restart(s, true) },
		"stop":    r.Stop,
	} {
		if err := op(mustRequire(t, "page")); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if n := engineCalls.Load(); n != 0 {
		t.Errorf("engine contacted %d time(s) for a daemon-served site", n)
	}
	if dirs := composeDirs(); len(dirs) != 0 {
		t.Errorf("compose ran for a daemon-served site in %q", dirs)
	}
}

// A batch pays for the Docker preconditions once, however many container
// sites it starts: one client for the ping, one for the network lookup.
func TestRunnerChecksDockerOncePerBatch(t *testing.T) {
	engineCalls, composeDirs := lifecycleFixture(t)
	r := Runner{Quiet: true}
	ready := 0
	r.OnDockerReady = func() { ready++ }

	var wg sync.WaitGroup
	for range 4 {
		for _, name := range []string{"blog", "page"} {
			s := mustRequire(t, name)
			wg.Go(func() {
				if err := r.Start(s, false); err != nil {
					t.Errorf("start %s: %v", name, err)
				}
			})
		}
	}
	wg.Wait()

	if n := engineCalls.Load(); n != 2 {
		t.Errorf("engine clients = %d, want 2 (one ping, one network lookup)", n)
	}
	if ready != 1 {
		t.Errorf("OnDockerReady ran %d times, want 1", ready)
	}
	cfg, _ := config.Load()
	want := SiteConfigDir(cfg, "blog")
	dirs := composeDirs()
	if len(dirs) != 4 {
		t.Fatalf("compose calls = %d, want 4 (blog only)", len(dirs))
	}
	for _, d := range dirs {
		if d != want {
			t.Errorf("compose ran in %q, want %q", d, want)
		}
	}
}

// Require is a lifecycle preamble: it must not spend an engine round trip
// probing container status the operation never reads.
func TestRequireSkipsStatusProbe(t *testing.T) {
	engineCalls, _ := lifecycleFixture(t)
	if s := mustRequire(t, "blog"); s.Status != "" {
		t.Errorf("Status = %q, want unprobed", s.Status)
	}
	if n := engineCalls.Load(); n != 0 {
		t.Errorf("Require contacted the engine %d time(s)", n)
	}
}
