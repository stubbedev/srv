package httpd

import (
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countRootOpens swaps openRoot for a counting wrapper.
func countRootOpens(t *testing.T) *atomic.Int64 {
	t.Helper()
	var n atomic.Int64
	prev := openRoot
	openRoot = func(dir string) (*os.Root, error) {
		n.Add(1)
		return prev(dir)
	}
	t.Cleanup(func() { openRoot = prev })
	return &n
}

// A site directory is opened once and reused, not reopened per request.
func TestSiteRootOpenedOncePerDirectory(t *testing.T) {
	opens := countRootOpens(t)
	root := t.TempDir()
	writeFile(t, root, "index.html", "hi")
	s := New("127.0.0.1:0")
	t.Cleanup(s.Shutdown)
	s.SetTargets(map[string]Target{"s.test": {Name: "s.test", Root: root}}, nil)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 25 {
				if code, _, _ := get(t, s, "s.test", "/"); code != http.StatusOK {
					t.Errorf("code = %d", code)
				}
			}
		})
	}
	wg.Wait()
	// Concurrent first requests may race to open; the loser closes its copy.
	if got := opens.Load(); got < 1 || got > 8 {
		t.Errorf("site root opened %d times for 200 requests, want once (or once per racing first request)", got)
	}
}

// A site directory replaced wholesale (moved aside, rebuilt in place) is
// served from the new directory once the recheck window passes.
func TestSiteRootFollowsReplacedDirectory(t *testing.T) {
	prev := rootRecheck
	rootRecheck = 10 * time.Millisecond
	t.Cleanup(func() { rootRecheck = prev })

	parent := t.TempDir()
	root := filepath.Join(parent, "site")
	writeFile(t, root, "index.html", "old")
	s := New("127.0.0.1:0")
	t.Cleanup(s.Shutdown)
	s.SetTargets(map[string]Target{"s.test": {Name: "s.test", Root: root}}, nil)
	if _, body, _ := get(t, s, "s.test", "/"); body != "old" {
		t.Fatalf("body = %q, want old", body)
	}

	if err := os.Rename(root, filepath.Join(parent, "site.old")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "index.html", "new")
	time.Sleep(3 * rootRecheck)
	if _, body, _ := get(t, s, "s.test", "/"); body != "new" {
		t.Errorf("body = %q after replacing the directory, want new", body)
	}

	// Removing the directory entirely answers 404, not stale content.
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * rootRecheck)
	if code, _, _ := get(t, s, "s.test", "/"); code != http.StatusNotFound {
		t.Errorf("code = %d after removing the directory, want 404", code)
	}
}

// Sites dropped from the host table release their cached directory.
func TestSetTargetsReleasesRemovedRoots(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "index.html", "hi")
	s := New("127.0.0.1:0")
	t.Cleanup(s.Shutdown)
	s.SetTargets(map[string]Target{"s.test": {Name: "s.test", Root: root}}, nil)
	get(t, s, "s.test", "/")
	s.SetTargets(map[string]Target{}, nil)
	s.roots.mu.Lock()
	n := len(s.roots.roots)
	s.roots.mu.Unlock()
	if n != 0 {
		t.Errorf("%d roots still cached after the site was removed", n)
	}
}

// The most specific wildcard site wins, at any subdomain depth.
func TestWildcardMostSpecificWins(t *testing.T) {
	s := New("127.0.0.1:0")
	s.SetTargets(nil, map[string]Target{
		"test":     {Name: "tld"},
		"app.test": {Name: "app"},
	})
	for host, want := range map[string]string{
		"x.test":       "tld",
		"x.app.test":   "app",
		"a.b.app.test": "app",
		"napp.test":    "tld",
	} {
		if got, ok := s.lookup(host); !ok || got.Name != want {
			t.Errorf("%s -> %q (ok=%v), want %q", host, got.Name, ok, want)
		}
	}
	for _, host := range []string{"test", "app.example.com", ""} {
		if got, ok := s.lookup(host); ok {
			t.Errorf("%q matched %q, want no target", host, got.Name)
		}
	}
}
