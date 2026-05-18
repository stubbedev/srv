package traefik

import (
	"os"
	"testing"

	"github.com/stubbedev/srv/internal/mkcert"
	"github.com/stubbedev/srv/internal/shell"
	"github.com/stubbedev/srv/internal/shell/shelltest"
)

// TestMain installs default fakes for the shell and mkcert runners so no test
// in this package can accidentally invoke real `sudo` or the embedded mkcert
// binary against the developer's machine. Tests are still free to swap in
// their own runners — per-test cleanup will restore the fakes installed here,
// not the real OS runners.
func TestMain(m *testing.M) {
	restoreShell := shell.SwapDefault(shelltest.New(nil))
	restoreMkcert := mkcert.SwapRunner(noopMkcertRunner{})
	code := m.Run()
	restoreMkcert()
	restoreShell()
	os.Exit(code)
}

type noopMkcertRunner struct{}

func (noopMkcertRunner) Stream(args ...string) error           { return nil }
func (noopMkcertRunner) Output(args ...string) ([]byte, error) { return nil, nil }
func (noopMkcertRunner) Combined(args ...string) ([]byte, error) {
	return []byte("The local CA is now installed in the system trust store!\n"), nil
}
