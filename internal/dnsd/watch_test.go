package dnsd

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// fakeWatcher replaces fsnotify for Watch-level tests: the first Add is
// signalled, every Add is recorded, and a test decides exactly which events
// arrive and when — nothing here waits on platform event latency.
type fakeWatcher struct {
	events chan fsnotify.Event
	errors chan error

	mu          sync.Mutex
	added       []string
	addedSignal chan struct{}
	addOnce     sync.Once
}

func newFakeWatcher() *fakeWatcher {
	return &fakeWatcher{
		events:      make(chan fsnotify.Event),
		errors:      make(chan error),
		addedSignal: make(chan struct{}),
	}
}

func (w *fakeWatcher) Add(dir string) error {
	w.mu.Lock()
	w.added = append(w.added, dir)
	w.mu.Unlock()
	w.addOnce.Do(func() { close(w.addedSignal) })
	return nil
}

func (w *fakeWatcher) Events() <-chan fsnotify.Event { return w.events }
func (w *fakeWatcher) Errors() <-chan error          { return w.errors }
func (w *fakeWatcher) Close() error                  { return nil }

func (w *fakeWatcher) adds() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.added)
}

// Watch must watch the zone files' directory — never the files themselves,
// which an atomic write replaces — and feed the watcher's events into the
// reload loop. Both are observable on the fake without a real filesystem
// backend in the picture: the add signal is a channel handshake, and the
// unbuffered event send returns only once the loop has received the event,
// so what follows (debounce, stat, reload) is code the test controls the
// timing of, not the platform.
func TestWatchWatchesZoneDirectoryAndFeedsEventsToLoop(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "dnsmasq.conf")
	hostsPath := filepath.Join(dir, "dnsmasq.hosts")
	for _, p := range []string{confPath, hostsPath} {
		if err := os.WriteFile(p, []byte(""), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := New("127.0.0.1", 0, confPath, hostsPath)
	if err != nil {
		t.Fatal(err)
	}
	fw := newFakeWatcher()
	s.newFileWatcher = func() (fileWatcher, error) { return fw, nil }
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		_ = s.Watch()
	}()
	t.Cleanup(func() {
		// Never served, so skip Shutdown's graceful wait on Serve.
		s.cancel()
		<-watchDone
		_ = s.conn.PacketConn.Close()
		_ = s.tcp.Listener.Close()
	})

	select {
	case <-fw.addedSignal:
	case <-watchDone:
		t.Fatal("Watch exited before installing any watch")
	}
	// confPath and hostsPath share a directory, so exactly one Add is right:
	// the directory, not the two files (an inode watch dies on rename-over).
	if got := fw.adds(); !slices.Equal(got, []string{dir}) {
		t.Fatalf("watched %v, want exactly the zone files' directory [%s]", got, dir)
	}

	renameOver(t, hostsPath, "127.0.0.1 watched.test\n")
	// A lone Remove for the zone file is kqueue's rename-over report; the
	// loop must reload from on-disk state regardless of op or name.
	fw.events <- fsnotify.Event{Name: hostsPath, Op: fsnotify.Remove}
	waitFor(t, "watcher event never reloaded the zone file", func() bool { return hasExact(s, "watched.test.") })
}

// loopServer builds a Server over two temp zone files and runs watchLoop on
// synthetic channels, so a test decides exactly which events arrive — the
// point being that the loop must not depend on which ones a backend sends.
func loopServer(t *testing.T, hosts string) (s *Server, hostsPath string, events chan fsnotify.Event) {
	t.Helper()
	dir := t.TempDir()
	confPath := filepath.Join(dir, "dnsmasq.conf")
	hostsPath = filepath.Join(dir, "dnsmasq.hosts")
	if err := os.WriteFile(confPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostsPath, []byte(hosts), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := New("127.0.0.1", 0, confPath, hostsPath)
	if err != nil {
		t.Fatal(err)
	}
	events = make(chan fsnotify.Event)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = s.watchLoop(events, make(chan error))
	}()
	t.Cleanup(func() {
		// Never served, so skip Shutdown's graceful wait on Serve.
		s.cancel()
		<-done
		_ = s.conn.PacketConn.Close()
		_ = s.tcp.Listener.Close()
	})
	return s, hostsPath, events
}

// renameOver replaces path the way fsutil.AtomicWriteFile does.
func renameOver(t *testing.T, path, content string) {
	t.Helper()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

func hasExact(s *Server, name string) bool {
	_, ok := s.zones.Load().exact[name]
	return ok
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal(what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Regression for #11: on kqueue a rename-over can arrive as a lone Remove for
// the zone file, with no Create. That must still reload.
func TestWatchLoopReloadsOnRemoveOnlyRenameOver(t *testing.T) {
	s, hostsPath, events := loopServer(t, "")
	renameOver(t, hostsPath, "127.0.0.1 watched.test\n")
	events <- fsnotify.Event{Name: hostsPath, Op: fsnotify.Remove}
	waitFor(t, "rename-over reported as Remove was never reloaded", func() bool { return hasExact(s, "watched.test.") })
}

// The event's name is not trusted either: any event in a watched directory
// triggers the stat check, e.g. only the temp file's events were delivered.
func TestWatchLoopReloadsOnEventForOtherName(t *testing.T) {
	s, hostsPath, events := loopServer(t, "")
	renameOver(t, hostsPath, "127.0.0.1 watched.test\n")
	events <- fsnotify.Event{Name: hostsPath + ".tmp", Op: fsnotify.Rename}
	waitFor(t, "rename-over was never reloaded", func() bool { return hasExact(s, "watched.test.") })
}

// A deleted zone file means its records are gone.
func TestWatchLoopDropsDeletedFile(t *testing.T) {
	s, hostsPath, events := loopServer(t, "127.0.0.1 gone.test\n")
	if !hasExact(s, "gone.test.") {
		t.Fatal("fixture record not loaded")
	}
	if err := os.Remove(hostsPath); err != nil {
		t.Fatal(err)
	}
	events <- fsnotify.Event{Name: hostsPath, Op: fsnotify.Remove}
	waitFor(t, "deleted zone file's records still served", func() bool { return !hasExact(s, "gone.test.") })
}

// Events that change neither file cost a stat, not a reload: the fallback
// snapshot must be the very same one afterwards.
func TestWatchLoopSkipsReloadWhenUnchanged(t *testing.T) {
	s, hostsPath, events := loopServer(t, "127.0.0.1 keep.test\n")
	// The unbuffered send returns once the loop is selecting, i.e. after its
	// initial reload, so the snapshot read next is the one it must keep.
	events <- fsnotify.Event{Name: filepath.Join(filepath.Dir(hostsPath), "unrelated"), Op: fsnotify.Create}
	before := s.fallback.Load()
	events <- fsnotify.Event{Name: hostsPath, Op: fsnotify.Chmod}
	time.Sleep(3 * reloadDebounce)
	if s.fallback.Load() != before {
		t.Error("zones reloaded although neither file changed")
	}
}

// A rename-over that keeps size and mtime is still a new file.
func TestZoneStampsSeeRenameOverWithSameSizeAndMtime(t *testing.T) {
	s, hostsPath, _ := loopServer(t, "127.0.0.1 aaaa.test\n")
	fi, err := os.Stat(hostsPath)
	if err != nil {
		t.Fatal(err)
	}
	before := s.statZones()
	renameOver(t, hostsPath, "127.0.0.1 bbbb.test\n")
	if err := os.Chtimes(hostsPath, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	if s.statZones().equal(before) {
		t.Error("rename-over with identical size and mtime compared equal")
	}
	if !s.statZones().equal(s.statZones()) {
		t.Error("unchanged files compared unequal")
	}
}
