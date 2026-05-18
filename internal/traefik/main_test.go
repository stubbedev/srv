package traefik

import (
	"os"
	"testing"

	"github.com/stubbedev/srv/internal/shell"
	"github.com/stubbedev/srv/internal/shell/shelltest"
)

// TestMain installs a default fake shell so no test in this package can
// accidentally invoke real `sudo` against the developer's machine. Tests are
// still free to swap in their own runners via shell.SwapDefault — per-test
// cleanup will restore the fake installed here, not the real OS runner.
func TestMain(m *testing.M) {
	restore := shell.SwapDefault(shelltest.New(nil))
	code := m.Run()
	restore()
	os.Exit(code)
}
