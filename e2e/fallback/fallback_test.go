//go:build e2e

// End-to-end coverage for the daemon-hosted fallback proxy, through real
// Traefik: `srv proxy add --fallback` writes a route at the persisted
// loopback listener, the failover proxy (started here the way the daemon's
// reconcile loop starts it) fronts a localhost primary, and a request that
// travels Traefik -> listener -> primary returns the primary's body until
// the primary dies — then the same request returns the fallback's, with no
// Traefik reload in between.
package fallback_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stubbedev/srv/e2e/harness"
	"github.com/stubbedev/srv/internal/fallbackd"
	"github.com/stubbedev/srv/internal/proxy"
)

func TestFallbackFailoverThroughTraefik(t *testing.T) {
	harness.SkipIfNoEngine(t)
	harness.SkipIfNoMkcert(t)
	harness.SkipIfPortsBusy(t)
	root := harness.NewRoot(t)
	harness.TraefikUp(t, root)

	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "primary body")
	}))
	t.Cleanup(primary.Close)
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "fallback body")
	}))
	t.Cleanup(fallback.Close)

	primaryPort := portOf(t, primary.URL)

	out := harness.RunSrv(t, root, "proxy", "add", "app.test",
		"--port", fmt.Sprint(primaryPort),
		"--fallback", fallback.URL)
	t.Logf("proxy add: %s", out)

	meta, err := proxy.Read("app-test")
	if err != nil || meta == nil {
		t.Fatalf("proxy metadata: %v", err)
	}
	if meta.FallbackPort <= 0 {
		t.Fatalf("metadata FallbackPort = %d, want an allocated port", meta.FallbackPort)
	}

	// Start the failover listener exactly the way the daemon's reconcile
	// loop does (same package, same metadata fields). The daemon is not
	// running in this test, so the suite plays that role directly.
	mgr := fallbackd.NewManager()
	t.Cleanup(mgr.Shutdown)
	addr, err := mgr.Ensure(fallbackd.Spec{
		Name:        meta.Name,
		PrimaryURL:  fmt.Sprintf("http://127.0.0.1:%d", meta.Port),
		FallbackURL: meta.FallbackURL,
		Timeout:     2 * time.Second,
		ListenPort:  meta.FallbackPort,
	})
	if err != nil {
		t.Fatalf("start fallback listener: %v", err)
	}
	t.Logf("failover listener on %s", addr)

	// Through Traefik, via the listener, to the live primary.
	harness.WaitForHTTPS(t, "app.test", "/", 30*time.Second, func(status int, body string) bool {
		return status == http.StatusOK && body == "primary body"
	})

	// Kill the primary: same route, same listener, now the fallback answers.
	primary.Close()
	harness.WaitForHTTPS(t, "app.test", "/", 30*time.Second, func(status int, body string) bool {
		return status == http.StatusOK && body == "fallback body"
	})
}

// portOf extracts the TCP port an httptest server is bound to.
func portOf(t *testing.T, rawURL string) int {
	t.Helper()
	var port int
	if _, err := fmt.Sscanf(rawURL, "http://127.0.0.1:%d", &port); err != nil {
		t.Fatalf("parse %s: %v", rawURL, err)
	}
	return port
}

// Keep os imported for the SRV_ROOT dump on failure paths.
var _ = os.Getenv
