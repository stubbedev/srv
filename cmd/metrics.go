// Package cmd — metrics.go implements `srv metrics` for managing the opt-in
// prometheus + grafana stack scraping Traefik.
package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/metrics"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

var metricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: "Manage the optional metrics stack (prometheus + grafana)",
	Long: `Prometheus scrapes Traefik's existing /metrics endpoint; Grafana ships with
a pre-wired Prometheus datasource. Both UIs route through Traefik with
mkcert-signed TLS:

    Grafana:     https://` + metrics.GrafanaDomain + `   (admin / admin)
    Prometheus:  https://` + metrics.PrometheusDomain + `

Import a Traefik dashboard in Grafana (dashboard ID 17347) to see request
rates, latency, and error percentages per router.`,
}

var metricsEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Render the metrics compose stack and start containers",
	Args:  cobra.NoArgs,
	RunE:  runMetricsEnable,
}

var metricsDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Stop and remove the metrics stack containers",
	Args:  cobra.NoArgs,
	RunE:  runMetricsDisable,
}

var metricsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether the metrics stack is running",
	Args:  cobra.NoArgs,
	RunE:  runMetricsStatus,
}

func init() {
	metricsCmd.GroupID = GroupSystem
	metricsCmd.AddCommand(metricsEnableCmd, metricsDisableCmd, metricsStatusCmd)
	RootCmd.AddCommand(metricsCmd)
}

func runMetricsEnable(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	domains := []string{metrics.GrafanaDomain, metrics.PrometheusDomain}

	// Issue / refresh a single mkcert cert that covers both hostnames.
	if _, err := traefik.EnsureLocalCert(metrics.ProxySiteName, domains, false); err != nil {
		ui.Warn("Failed to provision metrics certificate: %v", err)
	}
	for _, d := range domains {
		if err := traefik.RegisterLocalDomain(d, false); err != nil {
			ui.Warn("Failed to register DNS for %s: %v", d, err)
		}
	}

	if err := metrics.WriteStack(cfg); err != nil {
		return fmt.Errorf("render metrics stack: %w", err)
	}
	if err := metrics.WriteTraefikConfig(cfg); err != nil {
		return fmt.Errorf("write metrics Traefik config: %w", err)
	}
	if err := traefik.UpdateDynamicConfig(); err != nil {
		ui.Warn("Failed to refresh Traefik dynamic config: %v", err)
	}
	if err := docker.ComposeUp(metrics.Dir(cfg)); err != nil {
		return fmt.Errorf("start metrics stack: %w", err)
	}
	ui.Success("Metrics stack started")
	ui.Info("Grafana:    https://%s  (admin / admin)", metrics.GrafanaDomain)
	ui.Info("Prometheus: https://%s", metrics.PrometheusDomain)
	ui.Dim("Import Grafana dashboard ID 17347 for a Traefik overview.")
	return nil
}

func runMetricsDisable(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := docker.EnsureRunning(); err != nil {
		return err
	}
	if err := docker.ComposeDown(metrics.Dir(cfg)); err != nil {
		ui.Warn("Failed to stop metrics stack: %v", err)
	}
	if err := metrics.RemoveTraefikConfig(cfg); err != nil {
		ui.Warn("Failed to remove metrics Traefik config: %v", err)
	}
	for _, d := range []string{metrics.GrafanaDomain, metrics.PrometheusDomain} {
		if err := traefik.UnregisterLocalDomain(d); err != nil {
			ui.Warn("Failed to unregister DNS for %s: %v", d, err)
		}
	}
	if err := traefik.RemoveLocalCerts(metrics.ProxySiteName, metrics.GrafanaDomain); err != nil {
		ui.Warn("Failed to remove metrics certificate: %v", err)
	}
	if err := traefik.UpdateDynamicConfig(); err != nil {
		ui.Warn("Failed to refresh Traefik dynamic config: %v", err)
	}
	ui.Success("Metrics stack stopped")
	return nil
}

func runMetricsStatus(cmd *cobra.Command, args []string) error {
	promRunning := docker.IsContainerRunning(metrics.PrometheusContainer)
	grafanaRunning := docker.IsContainerRunning(metrics.GrafanaContainer)

	if promRunning {
		ui.Success("Prometheus: running (%s)", metrics.PrometheusContainer)
	} else {
		ui.Warn("Prometheus: not running")
	}
	if grafanaRunning {
		ui.Success("Grafana:    running (%s)", metrics.GrafanaContainer)
	} else {
		ui.Warn("Grafana:    not running")
	}

	if !promRunning && !grafanaRunning {
		ui.Blank()
		ui.Info("The metrics stack is not enabled. Start it with:")
		ui.Dim("    srv metrics enable")
		return nil
	}

	ui.Blank()
	ui.Bold("Open in your browser:")
	reportMetricsEndpoint("Grafana", metrics.GrafanaDomain)
	reportMetricsEndpoint("Prometheus", metrics.PrometheusDomain)

	ui.Blank()
	ui.Dim("Grafana login:      admin / admin")
	ui.Dim("Traefik dashboard:  in Grafana, Dashboards > New > Import > ID 17347")
	ui.Dim("Prometheus check:   query  traefik_entrypoint_requests_total  to confirm scraping")
	return nil
}

// reportMetricsEndpoint prints one metrics URL with a DNS + reachability probe
// so `srv metrics status` makes clear whether the UI is actually viewable —
// not just whether the container happens to be running.
func reportMetricsEndpoint(label, domain string) {
	url := "https://" + domain
	switch {
	case !traefik.CheckDNS(domain):
		ui.Warn("  %-12s %s  - DNS not resolving (run 'srv dns setup')", label, url)
	case metricsURLResponds(url):
		ui.Success("  %-12s %s", label, url)
	default:
		ui.Warn("  %-12s %s  - not responding yet (containers may still be starting)", label, url)
	}
}

// metricsURLResponds reports whether the URL returns a usable HTTP response.
// The mkcert cert is not in this process's trust store, so TLS verification is
// skipped — we only care that Traefik routed the request to a live backend.
// A 502 means Traefik could not reach the container, which is exactly the
// failure we want to surface, so it counts as "not responding".
//
// Every connection is dialed straight at Traefik on 127.0.0.1:443 so the probe
// does not depend on the system resolver (which, unlike a browser, may not
// route .local through dnsmasq for this process).
func metricsURLResponds(url string) bool {
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	client := &http.Client{
		Timeout: 4 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // local reachability probe only
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, "127.0.0.1:443")
			},
		},
	}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode != http.StatusBadGateway
}
