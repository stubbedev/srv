package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
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
	Short: "Run diagnostic checks",
	Long: `Run diagnostic checks to identify common issues with your srv setup.

Checks performed:
  - Docker availability and status
  - Required ports (80, 443, 8080)
  - Docker network existence
  - Traefik container status
  - Local SSL certificate validity
  - mkcert installation`,
	RunE: runDoctor,
}

func init() {
	doctorCmd.GroupID = GroupSystem
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
		{constants.PortHTTP, constants.PortNameHTTP},
		{constants.PortHTTPS, constants.PortNameHTTPS},
		{constants.PortDashboard, constants.PortNameDashboard},
		{constants.PortDNS, constants.PortNameDNS},
	}

	for _, p := range ports {
		if traefik.CheckPortAvailable(p.port) {
			ui.IndentedDim(1, ":%d (%s) - available", p.port, p.name)
		} else {
			// Check if it's our container using it
			if (p.port == constants.PortHTTP || p.port == constants.PortHTTPS || p.port == constants.PortDashboard) && traefik.IsRunning() {
				version := docker.GetContainerImageVersion(docker.ContainerTraefik)
				ui.IndentedSuccess(1, ":%d (%s) - in use by proxy [traefik:%s]", p.port, p.name, version)
			} else if p.port == constants.PortDNS && traefik.IsDNSRunning() {
				version := docker.GetContainerImageVersion(docker.ContainerDNS)
				ui.IndentedSuccess(1, ":%d (%s) - in use by dns [dnsmasq:%s]", p.port, p.name, version)
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

	// Check if there are any local domains registered
	localDomains, _ := traefik.LoadLocalDomains()
	hasLocalDomains := len(localDomains) > 0

	if traefik.IsDNSRunning() {
		ui.IndentedSuccess(1, "Container is running")

		if traefik.CheckDNS() {
			ui.IndentedSuccess(1, "Responding to queries")
		} else {
			ui.IndentedWarn(1, "Not responding to queries")
			issues++
		}

		// Only check system DNS if there are local domains that need it
		if hasLocalDomains {
			if traefik.CheckSystemDNS() {
				ui.IndentedSuccess(1, "System DNS configured")
			} else {
				ui.IndentedWarn(1, "System DNS not configured")
				ui.IndentedDim(1, "Try removing and re-adding a local site to trigger DNS setup")
				issues++
			}
		} else {
			ui.IndentedDim(1, "No local domains registered")
		}
	} else {
		// DNS container not running is only an issue if there are local domains
		if hasLocalDomains {
			ui.IndentedWarn(1, "Container is not running")
			ui.IndentedDim(1, "Run 'srv init' to start")
			issues++
		} else {
			ui.IndentedDim(1, "Not running (no local domains registered)")
		}
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
		ui.IndentedDim(1, "CA will be auto-installed on first 'srv add --local'")
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
		ui.IndentedDim(1, "No local certificates (generated when adding local sites)")
		return 0
	}

	expired := 0
	expiringSoon := 0
	for _, cert := range certs {
		if cert.IsExpired {
			expired++
		} else if cert.DaysLeft <= constants.CertExpiryWarningDays {
			expiringSoon++
		}
	}

	if expired > 0 {
		ui.IndentedError(1, "%d certificate(s) EXPIRED", expired)
		ui.IndentedDim(1, "Certificates auto-renew on 'srv start'")
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
	Short: "Update Traefik and DNS images",
	Long: `Pull the latest Traefik and DNS images and restart the containers.

This ensures you're running the latest versions with security
patches and new features.`,
	RunE: runUpdate,
}

func init() {
	updateCmd.GroupID = GroupSystem
	RootCmd.AddCommand(updateCmd)
}

func runUpdate(cmd *cobra.Command, args []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	// Pull both images
	ui.Info("Pulling latest images...")
	if err := docker.Pull(docker.ImageTraefik); err != nil {
		return fmt.Errorf("failed to pull Traefik image: %w", err)
	}
	if err := docker.Pull(docker.ImageDNS); err != nil {
		return fmt.Errorf("failed to pull DNS image: %w", err)
	}

	// Recreate containers if running
	if traefik.IsRunning() || traefik.IsDNSRunning() {
		ui.Info("Recreating containers...")
		if err := traefik.RecreateTraefik(); err != nil {
			return fmt.Errorf("failed to recreate containers: %w", err)
		}
		ui.Success("Traefik and DNS updated and restarted")
	} else {
		ui.Success("Images updated")
		ui.Dim("Run 'srv init' to start containers")
	}

	return nil
}

// =============================================================================
// version command
// =============================================================================

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version info",
	Run: func(cmd *cobra.Command, args []string) {
		ui.Info("srv %s", Version)
		if Commit != constants.DefaultCommit {
			ui.Dim("Commit: %s", Commit)
		}
		if BuildDate != constants.DefaultBuildDate {
			ui.Dim("Built:  %s", BuildDate)
		}
	},
}

func init() {
	RootCmd.AddCommand(versionCmd)
}
