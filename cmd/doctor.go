package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/firewall"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

// =============================================================================
// doctor command
// =============================================================================

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose common issues",
	Long: `Run diagnostic checks to identify common issues with your srv setup.

Checks performed:
  - Docker availability and status
  - Required ports (80, 443, 8080, 53)
  - Docker network existence
  - Traefik container status
  - DNS server status and configuration
  - Local SSL certificate validity
  - mkcert installation`,
	RunE: runDoctor,
}

func init() {
	RootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	ui.Blank()
	ui.Info("Running diagnostics...")
	ui.Blank()

	issues := 0
	issues += checkDocker()
	issues += checkFirewall()
	issues += checkPorts()
	issues += checkNetwork()
	issues += checkTraefik()
	issues += checkDNS()
	issues += checkCertificates()

	// Summary
	ui.Blank()
	if issues == 0 {
		ui.Success("All checks passed!")
	} else {
		ui.Warn("%d issue(s) found", issues)
	}
	ui.Blank()

	return nil
}

// checkDocker verifies Docker is running
func checkDocker() int {
	ui.Bold("Docker")
	if err := docker.EnsureRunning(); err != nil {
		ui.IndentedError(1, "Docker is not running or not installed")
		ui.Blank()
		return 1
	}
	ui.IndentedSuccess(1, "Docker is running")
	ui.Blank()
	return 0
}

// checkFirewall checks firewall status and port accessibility
func checkFirewall() int {
	issues := 0
	ui.Bold("Firewall")
	fwStatus := firewall.CheckPorts()

	if fwStatus.Firewall == firewall.FirewallNone {
		ui.IndentedDim(1, "No active firewall detected")
	} else {
		ui.IndentedDim(1, "Firewall: %s", firewall.Name(fwStatus.Firewall))
		if fwStatus.HTTPOpen {
			ui.IndentedSuccess(1, "Port 80 (HTTP) - open")
		} else {
			ui.IndentedWarn(1, "Port 80 (HTTP) - blocked")
			issues++
		}
		if fwStatus.HTTPSOpen {
			ui.IndentedSuccess(1, "Port 443 (HTTPS) - open")
		} else {
			ui.IndentedWarn(1, "Port 443 (HTTPS) - blocked")
			issues++
		}
		if !fwStatus.HTTPOpen || !fwStatus.HTTPSOpen {
			ui.IndentedDim(1, "Run 'srv init' to configure firewall")
		}
	}

	ui.Blank()
	return issues
}

// checkPorts verifies required ports are available or in use by srv
func checkPorts() int {
	issues := 0
	ui.Bold("Ports")

	type portInfo struct {
		port int
		name string
	}
	ports := []portInfo{
		{80, "HTTP"},
		{443, "HTTPS"},
		{8080, "Dashboard"},
		{53, "DNS"},
	}

	for _, p := range ports {
		if traefik.CheckPortAvailable(p.port) {
			ui.IndentedDim(1, ":%d (%s) - available", p.port, p.name)
		} else {
			// Check if it's our container using it
			if (p.port == 80 || p.port == 443 || p.port == 8080) && traefik.IsRunning() {
				ui.IndentedSuccess(1, ":%d (%s) - in use by Traefik", p.port, p.name)
			} else if p.port == 53 && traefik.IsDNSRunning() {
				ui.IndentedSuccess(1, ":%d (%s) - in use by srv-dns", p.port, p.name)
			} else {
				ui.IndentedWarn(1, ":%d (%s) - in use by another process", p.port, p.name)
				issues++
			}
		}
	}

	ui.Blank()
	return issues
}

// checkNetwork verifies Docker network exists
func checkNetwork() int {
	ui.Bold("Docker Network")
	cfg, err := config.Load()
	if err != nil {
		ui.IndentedError(1, "Failed to load config: %v", err)
		ui.Blank()
		return 1
	}

	if docker.NetworkExists(cfg.NetworkName) {
		ui.IndentedSuccess(1, "Network '%s' exists", cfg.NetworkName)
	} else {
		ui.IndentedWarn(1, "Network '%s' does not exist", cfg.NetworkName)
		ui.IndentedDim(1, "Run 'srv init' to create it")
		ui.Blank()
		return 1
	}

	ui.Blank()
	return 0
}

// checkTraefik verifies Traefik container is running
func checkTraefik() int {
	ui.Bold("Traefik")
	if traefik.IsRunning() {
		ui.IndentedSuccess(1, "Container is running")
		ui.Blank()
		return 0
	}

	ui.IndentedWarn(1, "Container is not running")
	ui.IndentedDim(1, "Run 'srv init' to start")
	ui.Blank()
	return 1
}

// checkDNS verifies DNS server status and configuration
func checkDNS() int {
	issues := 0
	ui.Bold("DNS Server")

	if traefik.IsDNSRunning() {
		ui.IndentedSuccess(1, "Container is running")

		if traefik.CheckDNS() {
			ui.IndentedSuccess(1, "Responding to queries")
		} else {
			ui.IndentedWarn(1, "Not responding to queries")
			issues++
		}

		if traefik.CheckSystemDNS() {
			ui.IndentedSuccess(1, "System DNS configured")
		} else {
			ui.IndentedWarn(1, "System DNS not configured")
			ui.IndentedDim(1, "Run 'srv dns setup' to configure")
			issues++
		}
	} else {
		ui.IndentedWarn(1, "Container is not running")
		ui.IndentedDim(1, "Run 'srv init' to start")
		issues++
	}

	ui.Blank()
	return issues
}

// checkCertificates verifies mkcert installation and certificate status
func checkCertificates() int {
	issues := 0
	ui.Bold("Local SSL Certificates")

	if err := traefik.CheckMkcert(); err != nil {
		ui.IndentedWarn(1, "mkcert is not installed")
		ui.IndentedDim(1, "Install mkcert for local HTTPS support")
		ui.Blank()
		return 1
	}

	ui.IndentedSuccess(1, "mkcert is installed")

	if traefik.IsCAInstalled() {
		ui.IndentedSuccess(1, "CA is installed in system trust store")
	} else {
		ui.IndentedWarn(1, "CA not installed")
		ui.IndentedDim(1, "Run 'srv trust' to install")
		issues++
	}

	issues += checkCertificateExpiry()

	ui.Blank()
	return issues
}

// checkCertificateExpiry checks for expired or expiring certificates
func checkCertificateExpiry() int {
	certs := traefik.ListLocalCerts()
	if len(certs) == 0 {
		ui.IndentedDim(1, "No local certificates (generated when adding .test/.local sites)")
		return 0
	}

	expired := 0
	expiringSoon := 0
	for _, cert := range certs {
		if cert.IsExpired {
			expired++
		} else if cert.DaysLeft <= 30 {
			expiringSoon++
		}
	}

	if expired > 0 {
		ui.IndentedError(1, "%d certificate(s) EXPIRED", expired)
		ui.IndentedDim(1, "Run 'srv trust --force' to regenerate")
		return 1
	}

	if expiringSoon > 0 {
		ui.IndentedWarn(1, "%d certificate(s) expiring soon", expiringSoon)
		return 1
	}

	ui.IndentedSuccess(1, "%d certificate(s) valid", len(certs))
	return 0
}

// =============================================================================
// update command
// =============================================================================

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update Traefik to the latest version",
	Long: `Pull the latest Traefik image and restart the container.

This ensures you're running the latest Traefik version with security
patches and new features.`,
	RunE: runUpdate,
}

func init() {
	RootCmd.AddCommand(updateCmd)
}

func runUpdate(cmd *cobra.Command, args []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	ui.Info("Pulling latest Traefik image...")
	if err := traefik.PullTraefikImage(); err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}

	if traefik.IsRunning() {
		ui.Info("Recreating Traefik container...")
		if err := traefik.RecreateTraefik(); err != nil {
			return fmt.Errorf("failed to restart Traefik: %w", err)
		}
		ui.Success("Traefik updated and restarted")
	} else {
		ui.Success("Traefik image updated")
		ui.Dim("Run 'srv init' to start Traefik")
	}

	return nil
}

// =============================================================================
// version command
// =============================================================================

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		ui.Info("srv %s", Version)
		if Commit != "none" {
			ui.Dim("Commit: %s", Commit)
		}
		if BuildDate != "unknown" {
			ui.Dim("Built:  %s", BuildDate)
		}
	},
}

func init() {
	RootCmd.AddCommand(versionCmd)
}
