//go:build e2e

// End-to-end coverage for the site leg: `srv add` on a static project writes
// the Traefik route + mkcert cert, Traefik's file provider hot-loads them,
// and a request matched by Host rule returns the site's index.
package site_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stubbedev/srv/e2e/harness"
)

func TestStaticSiteRouting(t *testing.T) {
	harness.SkipIfNoEngine(t)
	harness.SkipIfNoMkcert(t)
	harness.SkipIfPortsBusy(t)
	root := harness.NewRoot(t)
	harness.TraefikUp(t, root)

	project := filepath.Join(t.TempDir(), "www")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "index.html"), []byte("static e2e body"), 0o644); err != nil {
		t.Fatal(err)
	}

	harness.RunSrv(t, root, "add", project, "--domain", "site.test", "--local")

	harness.WaitForHTTPS(t, "site.test", "/", 30*time.Second, func(status int, body string) bool {
		return status == http.StatusOK && body == "static e2e body"
	})
}
