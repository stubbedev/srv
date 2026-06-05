package traefik

import (
	"fmt"
	"os"
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
//   - The shell and mkcert runners are faked, so no real `sudo` or mkcert
//     binary runs against the system trust store / resolver config.
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
	restoreMkcert := mkcert.SwapRunner(noopMkcertRunner{})
	// Pretend mkcert is on PATH so CheckMkcert() succeeds without a real
	// binary. Tests that want to assert "mkcert missing" override this
	// per-test via mkcert.SwapLookPath.
	restoreLookPath := mkcert.SwapLookPath(func(string) (string, error) { return "/fake/mkcert", nil })

	// Fake every path to the Docker daemon: the SDK client (status reads such
	// as IsDNSRunning), the `docker compose` subprocess, and `docker exec`.
	restoreClient := docker.SwapNewClientOK()
	restoreCompose := docker.SwapComposeExec(func(string, bool, ...string) error { return nil })
	restoreExec := docker.SwapDockerExec(func(bool, ...string) error { return nil })

	code := m.Run()

	restoreExec()
	restoreCompose()
	restoreClient()
	restoreLookPath()
	restoreMkcert()
	restoreShell()
	os.Unsetenv(constants.EnvSrvRoot)
	os.RemoveAll(root)
	os.Exit(code)
}

type noopMkcertRunner struct{}

func (noopMkcertRunner) Stream(args ...string) error           { return nil }
func (noopMkcertRunner) Output(args ...string) ([]byte, error) { return nil, nil }
func (noopMkcertRunner) Combined(args ...string) ([]byte, error) {
	return []byte("The local CA is now installed in the system trust store!\n"), nil
}
