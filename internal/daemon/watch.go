// Package daemon — watch.go runs a filesystem watcher that triggers
// site.Reload(name) whenever a metadata.yml is rewritten. Reloads are
// debounced per-site so editor save patterns (rename + chmod + write) do
// not produce multiple back-to-back reloads.
package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
)

// watchDebounce is the quiet period after the last metadata.yml event
// before a reload fires for that site. Editors typically write through
// 2–5 syscalls; 300ms is enough to coalesce them without delaying the
// user perceptibly.
const watchDebounce = 300 * time.Millisecond

// watchState holds the per-site debounce timer table guarded by mu.
type watchState struct {
	mu     sync.Mutex
	timers map[string]*time.Timer
	// reloadMu serialises Reload(name) calls for a given site so two close
	// edits cannot race against each other inside Reload.
	reloadMu sync.Map // map[string]*sync.Mutex
	count    int      // sites known to be watched (informational)
}

// startMetadataWatcher launches a goroutine that watches the sites directory
// for metadata.yml writes and dispatches site.Reload for each change. Returns
// the underlying watcher so the caller can close it on shutdown.
func (d *Daemon) startMetadataWatcher() (*fsnotify.Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// Watch the parent directory; metadata.yml lives one level deep
	// (~/.config/srv/sites/<name>/metadata.yml) so events surface as
	// changes to <name>/. Recursive watching is platform-specific, so we
	// add each existing site subdirectory plus the parent (to catch new
	// sites being added at runtime).
	if err := w.Add(d.cfg.SitesDir); err != nil {
		_ = w.Close()
		return nil, err
	}

	state := &watchState{timers: make(map[string]*time.Timer)}
	if err := state.addExistingSites(w, d.cfg.SitesDir); err != nil {
		d.log("Warning: failed to seed metadata watcher: %v", err)
	}
	d.log("Metadata watcher started (watching %d site dirs)", state.count)

	go d.watchLoop(w, state)
	return w, nil
}

// addExistingSites adds each existing site config dir to the watcher so
// subsequent metadata.yml writes are surfaced. New sites added later are
// picked up when a Create event lands on the parent SitesDir.
func (s *watchState) addExistingSites(w *fsnotify.Watcher, sitesDir string) error {
	entries, err := readDirSafe(sitesDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		if err := w.Add(filepath.Join(sitesDir, entry.Name())); err == nil {
			s.count++
		}
	}
	return nil
}

// watchLoop drains fsnotify events and dispatches debounced reloads.
func (d *Daemon) watchLoop(w *fsnotify.Watcher, state *watchState) {
	defer func() { _ = w.Close() }()

	for {
		select {
		case <-d.ctx.Done():
			return
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			d.log("Watcher error: %v", err)
		case event, ok := <-w.Events:
			if !ok {
				return
			}
			d.handleWatchEvent(w, state, event)
		}
	}
}

// handleWatchEvent processes a single fsnotify event.
func (d *Daemon) handleWatchEvent(w *fsnotify.Watcher, state *watchState, event fsnotify.Event) {
	// A new site directory was created under SitesDir: watch it.
	if event.Op&fsnotify.Create != 0 && isDirectChild(d.cfg.SitesDir, event.Name) {
		if !strings.HasPrefix(filepath.Base(event.Name), "_") {
			if err := w.Add(event.Name); err == nil {
				state.mu.Lock()
				state.count++
				state.mu.Unlock()
			}
		}
		return
	}

	// Only act on metadata.yml events.
	if filepath.Base(event.Name) != constants.MetadataFile {
		return
	}
	// Ignore Chmod-only events; they fire on touch / permission edits and
	// would otherwise reload on every save attempt that didn't change content.
	if event.Op == fsnotify.Chmod {
		return
	}
	siteName := filepath.Base(filepath.Dir(event.Name))
	if siteName == "" || strings.HasPrefix(siteName, "_") {
		return
	}
	state.scheduleReload(siteName, watchDebounce, func() {
		d.reloadSite(state, siteName)
	})
}

// scheduleReload sets (or resets) a debounce timer for the named site. The
// fire callback runs once per quiet period.
func (s *watchState) scheduleReload(siteName string, delay time.Duration, fire func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.timers[siteName]; ok {
		t.Stop()
	}
	s.timers[siteName] = time.AfterFunc(delay, func() {
		s.mu.Lock()
		delete(s.timers, siteName)
		s.mu.Unlock()
		fire()
	})
}

// reloadSite acquires the per-site mutex and dispatches site.Reload.
// Failures are logged but do not crash the daemon — the previously running
// site stays up.
func (d *Daemon) reloadSite(state *watchState, siteName string) {
	muAny, _ := state.reloadMu.LoadOrStore(siteName, &sync.Mutex{})
	mu, ok := muAny.(*sync.Mutex)
	if !ok {
		d.log("Reload %s: unexpected mutex type", siteName)
		return
	}
	mu.Lock()
	defer mu.Unlock()

	res, err := site.Reload(siteName)
	if err != nil {
		d.log("Reload %s: %v", siteName, err)
		return
	}
	if res.Skipped {
		// metadata.yml content hash matched last apply → nothing changed.
		return
	}
	for _, w := range res.Warnings {
		d.log("Reload %s warning: %s", siteName, w)
	}

	// Auto-restart on label/compose changes. `docker compose up -d` is
	// idempotent: only services whose compose-derived hash actually changed
	// get recreated, so this is safe to call after every reload.
	if res.NeedsRestart {
		s, err := site.GetByName(siteName)
		if err != nil || s == nil || s.IsBroken {
			d.log("Reload %s: container restart skipped (site missing or broken)", siteName)
			return
		}
		if err := docker.ComposeUpWithProfile(s.ComposeDir, s.Profile); err != nil {
			d.log("Reload %s: docker compose up failed: %v", siteName, err)
			return
		}
		d.log("Reload %s: artifacts regenerated and applied via compose up", siteName)
	} else {
		d.log("Reload %s: routing refreshed", siteName)
	}
}

// isDirectChild reports whether child is exactly one level below parent.
func isDirectChild(parent, child string) bool {
	return filepath.Dir(child) == parent
}

// readDirSafe returns the directory entries, swallowing the "does not exist"
// error so the daemon can start before any sites are registered.
func readDirSafe(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return entries, nil
}
