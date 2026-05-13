// Package cmd — metrics.go implements `srv metrics` for managing the opt-in
// prometheus + grafana stack scraping Traefik.
package cmd

import (
	"fmt"

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
	if docker.IsContainerRunning(metrics.PrometheusContainer) {
		ui.Success("Prometheus: running (%s)", metrics.PrometheusContainer)
	} else {
		ui.Warn("Prometheus: not running")
	}
	if docker.IsContainerRunning(metrics.GrafanaContainer) {
		ui.Success("Grafana:    running (%s)", metrics.GrafanaContainer)
	} else {
		ui.Warn("Grafana:    not running")
	}
	return nil
}
