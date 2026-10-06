package daemon

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/httpd"
	"github.com/stubbedev/srv/internal/site"
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
	useTestLog(t, d, logPath)

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
	useTestLog(t, d, logPath)
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

// A Create event for a new site directory must refresh the static server's
// host table too: previously only the container mapping was rebuilt, so a
// daemon-served site added by the CLI (whose metadata lands before the watch
// on its directory exists) stayed unresponsive until an unrelated reload.
func TestHandleWatchEventNewSiteDirRefreshesStaticHostTable(t *testing.T) {
	d, err := newDaemonForTest(t)
	if err != nil {
		t.Fatal(err)
	}
	defer d.cancel()
	if err := os.MkdirAll(d.cfg.SitesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "index.html"), []byte("news site"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := site.WriteSiteMetadata("news", site.SiteMetadata{
		Type:         site.SiteTypeStatic,
		Domains:      []string{"news.test"},
		ProjectPath:  project,
		DaemonServed: true,
	}); err != nil {
		t.Fatal(err)
	}

	srv := httpd.New("127.0.0.1:0")
	t.Cleanup(srv.Shutdown)
	d.static.Store(srv)

	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Skip("fsnotify unavailable")
	}
	defer w.Close()
	_ = w.Add(d.cfg.SitesDir)
	state := &watchState{timers: map[string]*time.Timer{}}
	newDir := filepath.Join(d.cfg.SitesDir, "news")
	d.handleWatchEvent(w, state, fsnotify.Event{Name: newDir, Op: fsnotify.Create})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://news.test/", nil)
	req.Host = "news.test"
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "news site" {
		t.Errorf("after Create event: code=%d body=%q — static host table was not refreshed", rec.Code, rec.Body.String())
	}
}

// stopAllTimers is the shutdown half of the debounce: an armed timer must
// not fire a site reload (and a compose up) once the daemon's context is
// cancelled.
func TestStopAllTimersCancelsPendingReloads(t *testing.T) {
	state := &watchState{timers: map[string]*time.Timer{}}
	fired := make(chan struct{}, 2)
	state.scheduleReload("a", 30*time.Millisecond, func() { fired <- struct{}{} })
	state.scheduleReload("b", 30*time.Millisecond, func() { fired <- struct{}{} })
	state.stopAllTimers()
	time.Sleep(80 * time.Millisecond)
	select {
	case <-fired:
		t.Fatal("debounce timer fired after stopAllTimers")
	default:
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.timers) != 0 {
		t.Errorf("timers = %d entries, want 0", len(state.timers))
	}
}
