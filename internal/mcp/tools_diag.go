package mcp

import (
	"context"
	"os"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/daemon"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/metrics"
)

// registerDiagTools binds read-only diagnostics tools: daemon and metrics-stack
// status plus the tail of the daemon log. These never mutate state.
func registerDiagTools(srv *mcpsdk.Server) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "daemon_status",
		Description: "Report whether the srv watch daemon is installed and running, plus its raw service-manager status (systemd/launchd). Call when sites are not hot-reloading or auto-connecting to the network.",
		Annotations: readOnlyAnno("Daemon status", true),
	}, daemonStatusTool)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "daemon_log",
		Description: "Return the tail of the daemon log (default 50 lines, override with `lines`). Use to see why reloads or container auto-connects failed.",
		Annotations: readOnlyAnno("Daemon log tail", true),
	}, daemonLogTool)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "metrics_status",
		Description: "Report whether the metrics stack (Prometheus + Grafana) containers are running, with their dashboard domains. Mirrors `srv metrics status`.",
		Annotations: readOnlyAnno("Metrics status", true),
	}, metricsStatusTool)
}

type daemonStatusIn struct{}
type daemonStatusOut struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Status    string `json:"status"`
	LogPath   string `json:"log_path,omitempty"`
}

func daemonStatusTool(_ context.Context, _ *mcpsdk.CallToolRequest, _ daemonStatusIn) (*mcpsdk.CallToolResult, daemonStatusOut, error) {
	out := daemonStatusOut{
		Installed: daemon.IsInstalled(),
		Running:   daemon.IsRunning(),
	}
	if status, err := daemon.ServiceStatus(); err == nil {
		out.Status = status
	} else {
		out.Status = "unknown"
	}
	if cfg, err := config.Load(); err == nil {
		out.LogPath = daemon.LogPath(cfg)
	}
	return nil, out, nil
}

type daemonLogIn struct {
	Lines int `json:"lines,omitempty"`
}
type daemonLogOut struct {
	Path  string   `json:"path"`
	Lines []string `json:"lines"`
}

func daemonLogTool(_ context.Context, _ *mcpsdk.CallToolRequest, in daemonLogIn) (*mcpsdk.CallToolResult, daemonLogOut, error) {
	n := in.Lines
	if n <= 0 {
		n = 50
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, daemonLogOut{}, err
	}
	path := daemon.LogPath(cfg)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, daemonLogOut{Path: path, Lines: []string{}}, nil
		}
		return nil, daemonLogOut{Path: path}, err
	}
	all := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return nil, daemonLogOut{Path: path, Lines: all}, nil
}

type metricsStatusIn struct{}
type metricsStatusOut struct {
	Enabled           bool   `json:"enabled"`
	PrometheusRunning bool   `json:"prometheus_running"`
	GrafanaRunning    bool   `json:"grafana_running"`
	GrafanaDomain     string `json:"grafana_domain"`
	PrometheusDomain  string `json:"prometheus_domain"`
}

func metricsStatusTool(_ context.Context, _ *mcpsdk.CallToolRequest, _ metricsStatusIn) (*mcpsdk.CallToolResult, metricsStatusOut, error) {
	prom := docker.IsContainerRunning(metrics.PrometheusContainer)
	graf := docker.IsContainerRunning(metrics.GrafanaContainer)
	return nil, metricsStatusOut{
		Enabled:           prom || graf,
		PrometheusRunning: prom,
		GrafanaRunning:    graf,
		GrafanaDomain:     metrics.GrafanaDomain,
		PrometheusDomain:  metrics.PrometheusDomain,
	}, nil
}
