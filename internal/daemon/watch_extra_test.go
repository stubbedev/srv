package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/stubbedev/srv/internal/config"
)

func TestAddExistingSites(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	config.ResetCache()
	t.Cleanup(config.ResetCache)
	sitesDir := filepath.Join(root, "sites")
	// Two normal site dirs + one internal _proxy dir.
	for _, name := range []string{"a", "b", "_proxy"} {
		if err := os.MkdirAll(filepath.Join(sitesDir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Skip("fsnotify unavailable")
	}
	defer w.Close()
	state := &watchState{timers: map[string]*time.Timer{}}
	if err := state.addExistingSites(w, sitesDir); err != nil {
		t.Fatal(err)
	}
	if state.count != 2 {
		t.Errorf("expected 2 sites watched, got %d", state.count)
	}
}

func TestAddExistingSitesMissing(t *testing.T) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Skip("fsnotify unavailable")
	}
	defer w.Close()
	state := &watchState{timers: map[string]*time.Timer{}}
	// readDirSafe returns nil for missing dir; addExistingSites should also be fine.
	if err := state.addExistingSites(w, "/nonexistent-srv-watch"); err != nil {
		t.Errorf("missing dir -> %v", err)
	}
}

func TestStartMetadataWatcher(t *testing.T) {
	d, err := newDaemonForTest(t)
	if err != nil {
		t.Fatal(err)
	}
	// Set up minimal log file path.
	logPath := filepath.Join(d.cfg.Root, "test.log")
	f, _ := os.Create(logPath)
	defer f.Close()
	d.logFile = f

	if err := os.MkdirAll(d.cfg.SitesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := d.startMetadataWatcher()
	if err != nil {
		t.Skipf("fsnotify unavailable: %v", err)
	}
	defer w.Close()
	d.cancel()
	// Give watchLoop a moment to exit.
	time.Sleep(50 * time.Millisecond)
}

func TestStartMetadataWatcherMissingSitesDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	config.ResetCache()
	t.Cleanup(config.ResetCache)
	d, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer d.cancel()
	// Sites dir doesn't exist → watcher.Add fails.
	if _, err := d.startMetadataWatcher(); err == nil {
		t.Error("expected err when sites dir missing")
	}
}

func TestHandleWatchEventNewSiteDir(t *testing.T) {
	d, err := newDaemonForTest(t)
	if err != nil {
		t.Fatal(err)
	}
	defer d.cancel()
	if err := os.MkdirAll(d.cfg.SitesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Skip("fsnotify unavailable")
	}
	defer w.Close()
	_ = w.Add(d.cfg.SitesDir)
	state := &watchState{timers: map[string]*time.Timer{}}

	// New site dir under SitesDir.
	newDir := filepath.Join(d.cfg.SitesDir, "blog")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	d.handleWatchEvent(w, state, fsnotify.Event{Name: newDir, Op: fsnotify.Create})
	if state.count != 1 {
		t.Errorf("expected count=1, got %d", state.count)
	}
}

func TestHandleWatchEventUnderscoreDir(t *testing.T) {
	d, err := newDaemonForTest(t)
	if err != nil {
		t.Fatal(err)
	}
	defer d.cancel()
	w, _ := fsnotify.NewWatcher()
	if w == nil {
		t.Skip("fsnotify unavailable")
	}
	defer w.Close()
	state := &watchState{timers: map[string]*time.Timer{}}
	d.handleWatchEvent(w, state, fsnotify.Event{Name: filepath.Join(d.cfg.SitesDir, "_proxy"), Op: fsnotify.Create})
	if state.count != 0 {
		t.Error("underscore dir should not be watched")
	}
}

func TestHandleWatchEventNonMetadata(t *testing.T) {
	d, err := newDaemonForTest(t)
	if err != nil {
		t.Fatal(err)
	}
	defer d.cancel()
	w, _ := fsnotify.NewWatcher()
	if w == nil {
		t.Skip("fsnotify unavailable")
	}
	defer w.Close()
	state := &watchState{timers: map[string]*time.Timer{}}
	// Non-metadata file change → no-op.
	d.handleWatchEvent(w, state, fsnotify.Event{Name: "/srv/x/other.txt", Op: fsnotify.Write})
}

func TestReloadSiteMissing(t *testing.T) {
	d, err := newDaemonForTest(t)
	if err != nil {
		t.Fatal(err)
	}
	defer d.cancel()
	logPath := filepath.Join(d.cfg.Root, "x.log")
	f, _ := os.Create(logPath)
	defer f.Close()
	d.logFile = f
	state := &watchState{timers: map[string]*time.Timer{}}
	d.reloadSite(state, "ghost")
}

func TestHandleWatchEventChmodIgnored(t *testing.T) {
	d, err := newDaemonForTest(t)
	if err != nil {
		t.Fatal(err)
	}
	defer d.cancel()
	w, _ := fsnotify.NewWatcher()
	if w == nil {
		t.Skip("fsnotify unavailable")
	}
	defer w.Close()
	state := &watchState{timers: map[string]*time.Timer{}}
	// Chmod-only event on metadata.yml → ignored.
	d.handleWatchEvent(w, state, fsnotify.Event{Name: "/srv/x/metadata.yml", Op: fsnotify.Chmod})
}
