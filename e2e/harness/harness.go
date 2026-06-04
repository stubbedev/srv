//go:build e2e

// Package harness drives a real Traefik instance (via docker compose) for
// srv's end-to-end tests. A test brings Traefik up against a throwaway
// SRV_ROOT, drives the real `srv` binary to register routes, then makes
// HTTP requests that travel through Traefik to assert routing actually
// works — not just that the right config files were written.
//
// Tests are gated behind `//go:build e2e` so the default `go test ./...`
// skips them. Run with `go test -tags=e2e ./e2e/...` (or `just test-e2e`).
//
// The suite needs the host ports Traefik binds (80/443/88/8080) to be free,
// so it self-skips when they're already in use (e.g. a developer running
// srv locally). CI runs on a clean host where they're available.
package harness

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/traefik"
)

// requiredPorts are the host ports Traefik binds under host networking on
// Linux. If any is already bound the suite can't run, so we skip.
var requiredPorts = []int{
	constants.PortHTTP,
	constants.PortHTTPS,
	constants.PortInternal,
	constants.PortDashboard,
}

// SkipIfNoDocker bails out when docker isn't on PATH or its daemon is
// unreachable, so machines without docker see a clean Skip.
func SkipIfNoDocker(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not installed")
	}
	cmd := exec.Command("docker", "info")
	if err := cmd.Run(); err != nil {
		t.Skipf("docker daemon not reachable: %v", err)
	}
}

// SkipIfNoMkcert skips when mkcert isn't installed. `srv proxy add` always
// issues a local cert, so mkcert is a hard dependency for the routing test.
func SkipIfNoMkcert(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("mkcert"); err != nil {
		t.Skip("mkcert not installed")
	}
}

// SkipIfPortsBusy skips when any Traefik port is already serving — probed by
// dialing rather than binding, so we never get a false "free" from a
// privileged-port permission error when running as non-root.
func SkipIfPortsBusy(t *testing.T) {
	t.Helper()
	for _, p := range requiredPorts {
		addr := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", p))
		conn, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			t.Skipf("port %d already in use — is srv/traefik already running here?", p)
		}
	}
}

var (
	buildOnce sync.Once
	builtPath string
	buildErr  error
)

// BuildSrv compiles the srv binary once per test run and returns its path.
// Building from the module (not a checked-in binary) keeps the e2e test
// honest about the current tree.
func BuildSrv(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "srv-e2e-bin")
		if err != nil {
			buildErr = err
			return
		}
		bin := dir + "/srv"
		cmd := exec.Command("go", "build", "-o", bin, "github.com/stubbedev/srv")
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			buildErr = fmt.Errorf("go build: %w", err)
			return
		}
		builtPath = bin
	})
	if buildErr != nil {
		t.Fatalf("build srv: %v", buildErr)
	}
	return builtPath
}

// NewRoot creates a throwaway SRV_ROOT, points this process's config layer
// at it, and returns the path. Cleanup restores the config cache.
func NewRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv(constants.EnvSrvRoot, root)
	config.ResetCache()
	t.Cleanup(config.ResetCache)
	return root
}

// TraefikUp writes Traefik config into root and starts ONLY the traefik
// service (the compose file also defines a dns service we don't need for
// routing tests). It registers a t.Cleanup that tears the stack down.
func TraefikUp(t *testing.T, root string) {
	t.Helper()

	if err := traefik.EnsureConfig(""); err != nil {
		t.Fatalf("traefik.EnsureConfig: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	// The compose file declares the srv network as external; create it so
	// compose validation passes. Idempotent.
	_ = exec.Command("docker", "network", "create", cfg.NetworkName).Run()

	compose := cfg.TraefikComposePath()
	up := exec.Command("docker", "compose", "-f", compose, "up", "-d", "--wait", "traefik")
	up.Stdout = os.Stderr
	up.Stderr = os.Stderr
	if err := up.Run(); err != nil {
		t.Fatalf("docker compose up traefik: %v", err)
	}

	t.Cleanup(func() {
		down := exec.Command("docker", "compose", "-f", compose, "down", "-v", "--remove-orphans")
		down.Stdout = os.Stderr
		down.Stderr = os.Stderr
		if err := down.Run(); err != nil {
			t.Logf("compose down: %v", err)
		}
		_ = exec.Command("docker", "network", "rm", cfg.NetworkName).Run()
	})
}

// RunSrv invokes the built srv binary with SRV_ROOT pinned to root and the
// given args. It returns combined output and fails the test on a non-zero
// exit (callers that expect failure should use RunSrvAllowErr).
func RunSrv(t *testing.T, root string, args ...string) string {
	t.Helper()
	out, err := RunSrvAllowErr(t, root, args...)
	if err != nil {
		t.Fatalf("srv %v failed: %v\n%s", args, err, out)
	}
	return out
}

// RunSrvAllowErr is RunSrv without the failure assertion.
func RunSrvAllowErr(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	bin := BuildSrv(t)
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), constants.EnvSrvRoot+"="+root)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// tlsClient is an HTTP client that routes every request to 127.0.0.1:443
// regardless of the request Host, and skips TLS verification (local mkcert
// certs aren't in the test process's trust store). This is how we drive
// Traefik's websecure entrypoint by Host rule without touching DNS.
func tlsClient() *http.Client {
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	return &http.Client{
		Timeout: 4 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // local mkcert cert, e2e only
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, "127.0.0.1:443")
			},
		},
	}
}

// GetHTTPS issues GET https://<host><path> through Traefik and returns the
// status code and body.
func GetHTTPS(t *testing.T, host, path string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "https://"+host+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Host = host
	resp, err := tlsClient().Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// WaitForHTTPS polls GET https://<host><path> until `check` passes on the
// (status, body) or timeout elapses. Traefik's file provider hot-reloads
// new routes within a second or two of `srv proxy add` writing them, but
// the exact moment isn't observable, so we poll.
func WaitForHTTPS(t *testing.T, host, path string, timeout time.Duration, check func(status int, body string) bool) (int, string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var status int
	var body string
	for time.Now().Before(deadline) {
		status, body = GetHTTPS(t, host, path)
		if check(status, body) {
			return status, body
		}
		time.Sleep(500 * time.Millisecond)
	}
	return status, body
}
