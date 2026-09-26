//go:build e2e

// End-to-end coverage for daemon-served static sites: `srv add --daemon`
// writes a file-provider route to the daemon's embedded static server (no
// container exists), Traefik hot-loads it, and the standalone `srv httpd`
// process — the same server the daemon embeds — answers by Host rule. Stop
// removes the route, so the domain 404s like a stopped container's.
package routing_test

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stubbedev/srv/e2e/harness"
	"github.com/stubbedev/srv/internal/constants"
)

func TestDaemonServedStaticSiteRouting(t *testing.T) {
	// Drives the same Traefik boot as the site suite; the docker leg is the
	// deliverable (see TestStaticSiteRouting for the podman split). The add
	// itself is engine-independent — that is the point of --daemon — but this
	// suite asserts routing, which needs the Traefik stack either way.
	if os.Getenv("SRV_CONTAINER_ENGINE") == "podman" {
		t.Skip("traefik boot parity on the podman leg is tracked with the site suite")
	}
	harness.SkipIfNoEngine(t)
	harness.SkipIfNoMkcert(t)
	harness.SkipIfPortsBusy(t)
	root := harness.NewRoot(t)
	harness.TraefikUp(t, root)

	project := filepath.Join(t.TempDir(), "page")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "index.html"), []byte("daemon e2e body"), 0o644); err != nil {
		t.Fatal(err)
	}

	harness.RunSrv(t, root, "add", project, "--domain", "daemon.test", "--name", "daemon-test", "--local", "--daemon")

	// The embedded static server runs in the daemon; the standalone form is
	// the same code against the same SRV_ROOT.
	bin := harness.BuildSrv(t)
	httpd := exec.Command(bin, "httpd")
	httpd.Env = append(os.Environ(), constants.EnvSrvRoot+"="+root)
	httpd.Stdout = os.Stderr
	httpd.Stderr = os.Stderr
	if err := httpd.Start(); err != nil {
		t.Fatalf("start srv httpd: %v", err)
	}
	t.Cleanup(func() {
		_ = httpd.Process.Signal(syscall.SIGTERM)
		_ = httpd.Wait()
	})

	harness.WaitForHTTPS(t, "daemon.test", "/", 30*time.Second, func(status int, body string) bool {
		return status == http.StatusOK && body == "daemon e2e body"
	})

	harness.RunSrv(t, root, "stop", "daemon-test")
	harness.WaitForHTTPS(t, "daemon.test", "/", 15*time.Second, func(status int, body string) bool {
		return status == http.StatusNotFound
	})
}
