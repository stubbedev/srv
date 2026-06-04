//go:build e2e

package proxy_test

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/srv/e2e/harness"
)

// TestProxyRoutesToLocalhostUpstream is the core routing e2e: it stands up a
// real Traefik, registers a proxy from a .test domain to an in-process HTTP
// upstream via the real `srv proxy add` CLI, then asserts a request to
// Traefik's websecure entrypoint (matched by Host rule) is forwarded to the
// upstream and returns its body.
//
// This exercises the whole chain end to end:
//   - srv proxy add → writes the file-provider router + mkcert cert
//   - Traefik file provider → hot-loads the router and TLS cert
//   - host-network Traefik → reaches the localhost upstream
func TestProxyRoutesToLocalhostUpstream(t *testing.T) {
	harness.SkipIfNoDocker(t)
	harness.SkipIfNoMkcert(t)
	harness.SkipIfPortsBusy(t)

	const domain = "e2e-proxy.test"
	const want = "hello-from-e2e-upstream"

	// In-process upstream on a random loopback port. Traefik runs with
	// host networking on Linux, so it can dial 127.0.0.1:<port> directly.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen upstream: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(want))
		}),
		ReadHeaderTimeout: 2 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	root := harness.NewRoot(t)
	harness.TraefikUp(t, root)

	harness.RunSrv(t, root,
		"proxy", "add",
		"--domain", domain,
		"--port", fmt.Sprintf("%d", port),
		"--name", "e2eproxy",
	)

	status, body := harness.WaitForHTTPS(t, domain, "/", 30*time.Second, func(status int, body string) bool {
		return status == http.StatusOK && strings.Contains(body, want)
	})

	if status != http.StatusOK {
		t.Fatalf("expected 200 from Traefik, got %d (body=%q)", status, body)
	}
	if !strings.Contains(body, want) {
		t.Fatalf("upstream body not routed through Traefik: got %q, want substring %q", body, want)
	}
}
