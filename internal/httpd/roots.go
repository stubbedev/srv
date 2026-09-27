package httpd

import (
	"io/fs"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// rootRecheck is how often a cached site root is re-validated against the
// path it was opened from. A site directory replaced wholesale (moved away
// and recreated, a symlink repointed) is picked up within this window; the
// requests in between are served from the handle already open.
var rootRecheck = time.Second

// rootRetireGrace is how long a replaced root handle stays open for requests
// that fetched it just before the swap. Files a request already opened stay
// valid after the root closes; only a later Open through the old root would
// fail, and requests do all their opens within milliseconds of starting.
var rootRetireGrace = time.Minute

// openRoot is os.OpenRoot; tests swap it to count directory opens.
var openRoot = os.OpenRoot

// siteRoot is one cached, open site directory.
type siteRoot struct {
	root *os.Root
	info fs.FileInfo // identity of the directory the handle points at
	// checked is the UnixNano of the last validation; one request per
	// rootRecheck window wins the CAS and re-stats the path.
	checked atomic.Int64
}

// rootCache keeps each site directory open across requests: opening the
// directory per request costs syscalls on every hit, and the handle is
// reusable because os.Root resolves every name relative to it.
type rootCache struct {
	mu    sync.Mutex
	roots map[string]*siteRoot
}

func newRootCache() *rootCache {
	return &rootCache{roots: make(map[string]*siteRoot)}
}

// get returns an open root for dir, reopening it when the path no longer
// names the directory the cached handle points at.
func (c *rootCache) get(dir string) (*os.Root, error) {
	c.mu.Lock()
	cur := c.roots[dir]
	c.mu.Unlock()

	now := time.Now().UnixNano()
	if cur != nil {
		last := cur.checked.Load()
		if now-last < int64(rootRecheck) || !cur.checked.CompareAndSwap(last, now) {
			return cur.root, nil
		}
		if st, err := os.Stat(dir); err == nil && os.SameFile(st, cur.info) {
			return cur.root, nil
		}
	}

	root, err := openRoot(dir)
	if err != nil {
		c.replace(dir, cur, nil)
		return nil, err
	}
	info, err := root.Stat(".")
	if err != nil {
		_ = root.Close()
		c.replace(dir, cur, nil)
		return nil, err
	}
	fresh := &siteRoot{root: root, info: info}
	fresh.checked.Store(now)
	if winner := c.replace(dir, cur, fresh); winner != fresh {
		// Another request swapped in its own handle first; use that one.
		_ = root.Close()
		if winner == nil {
			return nil, fs.ErrNotExist
		}
		return winner.root, nil
	}
	return root, nil
}

// replace swaps dir's entry from old to next (nil deletes it) when old is
// still current, retiring old, and returns the entry now installed.
func (c *rootCache) replace(dir string, old, next *siteRoot) *siteRoot {
	c.mu.Lock()
	defer c.mu.Unlock()
	if installed := c.roots[dir]; installed != old {
		return installed
	}
	if next == nil {
		delete(c.roots, dir)
	} else {
		c.roots[dir] = next
	}
	if old != nil {
		retire(old.root)
	}
	return next
}

// keepOnly retires every cached root whose directory is not in dirs, so
// removed sites do not pin their directories open.
func (c *rootCache) keepOnly(dirs map[string]bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for dir, r := range c.roots {
		if !dirs[dir] {
			delete(c.roots, dir)
			retire(r.root)
		}
	}
}

// closeAll closes every cached root immediately (server shutdown).
func (c *rootCache) closeAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for dir, r := range c.roots {
		delete(c.roots, dir)
		_ = r.root.Close()
	}
}

func retire(root *os.Root) {
	time.AfterFunc(rootRetireGrace, func() { _ = root.Close() })
}
