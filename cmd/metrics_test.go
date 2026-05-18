package cmd

import (
	"errors"
	"testing"

	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/mkcert"
)

func TestRunMetricsStatusNotRunning(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientErr(errors.New("offline")))
	if err := runMetricsStatus(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunMetricsEnableDockerDown(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientErr(errors.New("offline")))
	if err := runMetricsEnable(nil, nil); err == nil {
		t.Error("expected err when docker is unavailable")
	}
}

func TestRunMetricsDisableDockerDown(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientErr(errors.New("offline")))
	if err := runMetricsDisable(nil, nil); err == nil {
		t.Error("expected err when docker is unavailable")
	}
}

func TestReportMetricsEndpoint(t *testing.T) {
	// Exercise the function. DNS won't resolve in CI; that's fine — we just
	// ensure all switch cases run without panicking.
	reportMetricsEndpoint("Grafana", "grafana.local")
}

func TestMetricsURLResponds(t *testing.T) {
	// Probes 127.0.0.1:443 — result depends on env, so just exercise.
	_ = metricsURLResponds("https://grafana.local")
}

func TestRunMetricsEnableHappy(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientOK())
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	t.Cleanup(mkcert.SwapRunner(stubMkcertRunner{}))
	if err := runMetricsEnable(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunMetricsDisableHappy(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientOK())
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	if err := runMetricsDisable(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

// stubMkcertRunner returns enough output for traefik certificate paths to
// succeed when invoked from cmd-level tests.
type stubMkcertRunner struct{}

func (stubMkcertRunner) Stream(args ...string) error           { return nil }
func (stubMkcertRunner) Output(args ...string) ([]byte, error) { return []byte("/tmp/mkcert\n"), nil }
func (stubMkcertRunner) Combined(args ...string) ([]byte, error) {
	return []byte("Created a new local CA"), nil
}
