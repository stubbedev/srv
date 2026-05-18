package cmd

import (
	"errors"
	"os"
	"testing"

	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/mkcert"
	"github.com/stubbedev/srv/internal/shell"
	"github.com/stubbedev/srv/internal/shell/shelltest"
)

// TestMain installs default fakes for the shell, mkcert, and docker compose
// runners so no test in this package can accidentally invoke real `sudo`, the
// embedded mkcert binary, or `docker compose` against the developer's machine.
// Individual tests are still free to swap in their own runners; the per-test
// cleanup will restore the fakes installed here, not the real OS runners.
func TestMain(m *testing.M) {
	restoreShell := shell.SwapDefault(shelltest.New(nil))
	restoreMkcert := mkcert.SwapRunner(stubMkcertRunner{})
	restoreCompose := docker.SwapComposeExec(func(string, bool, ...string) error {
		return errors.New("docker compose disabled in tests; SwapComposeExec in your test")
	})
	restoreDocker := docker.SwapNewClientErr(errors.New("docker disabled in tests; SwapNewClientOK/WithNetwork in your test"))

	code := m.Run()

	restoreDocker()
	restoreCompose()
	restoreMkcert()
	restoreShell()
	os.Exit(code)
}
