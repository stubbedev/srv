package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/mkcert"
	"github.com/stubbedev/srv/internal/shell"
	"github.com/stubbedev/srv/internal/shell/shelltest"
)

// TestMain installs default fakes for the shell, mkcert, and docker compose
// runners so no test in this package can accidentally invoke real `sudo`, the
// trust-store install, or `docker compose` against the developer's machine.
// The mkcert engine is vendored: CAROOT is pointed into a temp directory so
// certificate issuance stays real but never leaves the sandbox, and the
// engine swap stubs out only the privileged trust-store operations.
// Individual tests are still free to swap in their own engines; the per-test
// cleanup will restore the fakes installed here, not the real OS runners.
func TestMain(m *testing.M) {
	caroot, err := os.MkdirTemp("", "srv-cmd-caroot-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: create temp CAROOT: %v\n", err)
		os.Exit(1)
	}
	os.Setenv("CAROOT", caroot)

	restoreShell := shell.SwapDefault(shelltest.New(nil))
	restoreEngine := mkcert.SwapEngine(&stubMkcertEngine{})
	restoreCompose := docker.SwapComposeExec(func(string, bool, ...string) error {
		return errors.New("docker compose disabled in tests; SwapComposeExec in your test")
	})
	restoreDocker := docker.SwapNewClientErr(errors.New("docker disabled in tests; SwapNewClientOK/WithNetwork in your test"))

	code := m.Run()

	restoreDocker()
	restoreCompose()
	restoreEngine()
	restoreShell()
	os.Unsetenv("CAROOT")
	os.RemoveAll(caroot)
	os.Exit(code)
}

// stubMkcertEngine is the package-wide mkcert.Engine fake: certificate
// issuance is delegated to the real engine (CAROOT lives in a temp dir),
// while trust-store installs return a happy result instead of touching the
// host. Tests inject failures by swapping in their own instance with
// installErr/issueErr set.
type stubMkcertEngine struct {
	installRes mkcert.InstallResult
	installErr error
	issueErr   error
}

func (s *stubMkcertEngine) Available() bool { return true }

func (s *stubMkcertEngine) Install() (mkcert.InstallResult, error) {
	if s.installRes == (mkcert.InstallResult{}) {
		s.installRes = mkcert.InstallResult{
			NewCA:         true,
			SystemTrustOK: true,
			CARootPath:    filepath.Join(mkcert.CAROOT(), "rootCA.pem"),
		}
	}
	return s.installRes, s.installErr
}

func (s *stubMkcertEngine) Uninstall() error { return nil }

func (s *stubMkcertEngine) IssueCert(certPath, keyPath string, domains []string) error {
	if s.issueErr != nil {
		return s.issueErr
	}
	return mkcert.SystemEngine().IssueCert(certPath, keyPath, domains)
}
