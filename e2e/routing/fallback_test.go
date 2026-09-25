//go:build e2e

// End-to-end coverage for proxy fallback, through real Traefik: `srv proxy
// add --fallback` renders a native Traefik failover service, and a request
// that travels Traefik -> primary returns the primary's body until the
// primary dies — then the same request returns the fallback's, with no
// Traefik reload and no extra proxy or container in between.
package routing_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stubbedev/srv/e2e/harness"
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

	out := harness.RunSrv(t, root, "proxy", "add", "--domain", "app.test",
		"--port", fmt.Sprint(primaryPort),
		"--fallback", fallback.URL)
	t.Logf("proxy add: %s", out)

	meta, err := proxy.Read("app-test")
	if err != nil || meta == nil {
		t.Fatalf("proxy metadata: %v", err)
	}
	if meta.FallbackURL == "" {
		t.Fatalf("metadata FallbackURL is empty, want the fallback recorded")
	}
	if meta.FallbackPort != 0 {
		t.Errorf("metadata FallbackPort = %d, want 0 — the retired daemon-hosted listener must not come back", meta.FallbackPort)
	}

	// Through Traefik to the live primary.
	harness.WaitForHTTPS(t, "app.test", "/", 30*time.Second, func(status int, body string) bool {
		return status == http.StatusOK && body == "primary body"
	})

	// Kill the primary: the same route now serves the fallback's body —
	// Traefik sees the dial failure as a 502 and fails over per request.
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
