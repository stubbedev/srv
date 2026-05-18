package cmd

import (
	"os"
	"testing"

	"github.com/stubbedev/srv/internal/mkcert"
	"github.com/stubbedev/srv/internal/shell"
	"github.com/stubbedev/srv/internal/shell/shelltest"
)

// TestMain installs default fakes for the shell and mkcert runners so no test
// in this package can accidentally invoke real `sudo` or the embedded mkcert
// binary against the developer's machine. Individual tests are still free to
// swap in their own runners via shell.SwapDefault / mkcert.SwapRunner — the
// per-test cleanup will restore the fake installed here, not the real OS
// runner.
func TestMain(m *testing.M) {
	restoreShell := shell.SwapDefault(shelltest.New(nil))
	restoreMkcert := mkcert.SwapRunner(stubMkcertRunner{})

	code := m.Run()

	restoreMkcert()
	restoreShell()
	os.Exit(code)
}
