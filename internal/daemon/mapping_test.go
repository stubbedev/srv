package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
)

// countSiteScans swaps listSites for a counting wrapper.
func countSiteScans(t *testing.T) *int {
	t.Helper()
	n := 0
	prev := listSites
	listSites = func() ([]site.Site, error) {
		n++
		return prev()
	}
	t.Cleanup(func() { listSites = prev })
	return &n
}

func startEvent(name string) docker.Event {
	return docker.Event{Actor: docker.EventActor{Attributes: map[string]string{"name": name}}}
}

// With the metadata watcher pushing refreshes, a flood of foreign container
// starts (a CI run, a load test) must cost map probes, never a re-read of
// every site's metadata.
func TestForeignContainerStartsDoNotScanSites(t *testing.T) {
	d, err := newDaemonForTest(t)
	if err != nil {
		t.Fatal(err)
	}
	useTestLog(t, d, filepath.Join(d.cfg.Root, "x.log"))
	if err := os.MkdirAll(d.cfg.SitesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	d.watching.Store(true)
	scans := countSiteScans(t)

	for i := range 100 {
		d.handleContainerStart(startEvent("foreign-" + string(rune('a'+i%26))))
	}
	// The first miss may take one scan to record the sites dir state.
	if *scans > 1 {
		t.Errorf("%d site scans for 100 foreign container starts, want at most 1", *scans)
	}

	// A new site directory is the one change the watcher can race; the
	// next miss must notice it.
	before := *scans
	if err := os.MkdirAll(filepath.Join(d.cfg.SitesDir, "newsite"), 0o755); err != nil {
		t.Fatal(err)
	}
	d.handleContainerStart(startEvent("foreign-z"))
	if *scans != before+1 {
		t.Errorf("new site dir: %d scans, want exactly one more than %d", *scans, before)
	}
}

// Without the watcher the old throttle applies: at most one scan per
// cooldown however many foreign containers start.
func TestForeignContainerStartsThrottledWithoutWatcher(t *testing.T) {
	d, err := newDaemonForTest(t)
	if err != nil {
		t.Fatal(err)
	}
	useTestLog(t, d, filepath.Join(d.cfg.Root, "x.log"))
	scans := countSiteScans(t)
	for range 100 {
		d.handleContainerStart(startEvent("foreign"))
	}
	if *scans != 1 {
		t.Errorf("%d scans for 100 starts within one cooldown, want 1", *scans)
	}
}
