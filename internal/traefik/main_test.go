package traefik

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/mkcert"
	"github.com/stubbedev/srv/internal/shell"
	"github.com/stubbedev/srv/internal/shell/shelltest"
)

// TestMain installs default fakes so no test in this package can touch the
// developer's real srv install. Three classes of leak are sealed off:
//
//   - SRV_ROOT is pointed at a throwaway temp dir, so config.Load() and every
//     file write (dnsmasq.conf, compose files, hostsdir) land there instead of
//     ~/.config/srv. config.Load() caches the root on first call, so this must
//     be set before m.Run.
//   - The shell runner is faked and CAROOT is pointed into the temp root, so
//     no real `sudo` runs and certificate issuance stays in the sandbox. The
//     mkcert engine swap stubs the trust-store touchpoints; cert generation
//     itself is delegated to the real vendored engine.
//   - The Docker SDK client and the compose/exec subprocess seams are faked,
//     so IsDNSRunning() reports false and ReloadDNS() cannot recreate the real
//     srv_dns container. (Previously, on a machine with srv installed, the DNS
//     tests force-recreated the live srv_dns container on every run.)
//
// Tests are still free to swap in their own runners — per-test cleanup restores
// the fakes installed here, not the real OS/Docker runners.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "srv-traefik-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: create temp SRV_ROOT: %v\n", err)
		os.Exit(1)
	}
	os.Setenv(constants.EnvSrvRoot, root)

	restoreShell := shell.SwapDefault(shelltest.New(nil))
	os.Setenv("CAROOT", filepath.Join(root, "caroot"))
	restoreEngine := mkcert.SwapEngine(&stubMkcertEngine{})

	// Fake every path to the Docker daemon: the SDK client (status reads such
	// as IsDNSRunning), the `docker compose` subprocess, and `docker exec`.
	restoreClient := docker.SwapNewClientOK()
	restoreCompose := docker.SwapComposeExec(func(string, bool, ...string) error { return nil })
	restoreExec := docker.SwapDockerExec(func(bool, ...string) error { return nil })

	code := m.Run()

	restoreExec()
	restoreCompose()
	restoreClient()
	restoreEngine()
	restoreShell()
	os.Unsetenv(constants.EnvSrvRoot)
	os.Unsetenv("CAROOT")
	os.RemoveAll(root)
	os.Exit(code)
}

// stubMkcertEngine is the package-wide mkcert.Engine fake: certificate
// issuance is delegated to the real engine (CAROOT lives in the temp SRV_ROOT)
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
