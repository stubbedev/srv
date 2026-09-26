package cmd

import (
	"errors"
	"os"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/metrics"
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
	t.Cleanup(mkcert.SwapEngine(&stubMkcertEngine{}))
	if err := runMetricsEnable(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunMetricsDisableHappy(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientOK())
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	if err := runMetricsEnable(nil, nil); err != nil {
		t.Fatalf("enable: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !metrics.IsConfigured(cfg) {
		t.Fatal("expected the rendered stack on disk after enable")
	}
	if err := runMetricsDisable(nil, nil); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if metrics.IsConfigured(cfg) {
		t.Error("disable must remove the rendered stack, otherwise srv install and srv doctor still treat metrics as enabled")
	}
	if _, err := os.Stat(metrics.Dir(cfg)); !os.IsNotExist(err) {
		t.Errorf("metrics dir should be gone after disable, stat err: %v", err)
	}
}
