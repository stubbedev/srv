// Package cmd — site_add_finalize.go finishes the `srv add` flow after the
// on-disk artifacts have been written: issue local certs + register DNS,
// surface a summary, and start the containers.
package cmd

import (
	"errors"
	"fmt"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

// finalizeSiteSetup handles SSL certs and optional start
func finalizeSiteSetup(cfg *config.Config, setup *siteSetup) error {
	// Generate SSL certificate for local domains
	if setup.isLocal {
		generateLocalCert(setup.siteName, setup.allDomains(), setup.wildcard)
	}

	// Determine site type label
	var siteType string
	switch {
	case setup.isNode:
		siteType = "node"
	case setup.isRuby:
		siteType = "ruby"
	case setup.isPython:
		siteType = "python"
	case setup.isDockerfile:
		siteType = "dockerfile"
	case setup.isStatic:
		siteType = "static"
	default:
		siteType = "compose"
	}

	ui.Success("Site '%s' added successfully!", setup.siteName)
	ui.Dim("Domain: %s (%s, %s)", setup.domain, siteType, ui.Highlight(TypeLabel(setup.isLocal)))
	ui.Dim("Config: %s/sites/%s/ (no project files modified)", cfg.Root, setup.siteName)

	// Always start the site after adding
	return startSiteAfterAdd(cfg, setup)
}

// generateLocalCert generates SSL certificate for local domains and registers DNS
// for every supplied domain. DNS registration always runs regardless of whether
// mkcert is available — TLS and DNS are independent concerns.
func generateLocalCert(siteName string, domains []string, wildcard bool) {
	if len(domains) == 0 {
		return
	}
	primary := domains[0]

	// Always register DNS — this must happen even if mkcert is missing.
	for _, d := range domains {
		if err := traefik.RegisterLocalDomain(d, wildcard); err != nil {
			ui.Warn("Failed to register DNS for %s: %v", d, err)
		}
	}
	ui.Dim("If a domain doesn't resolve, clear your browser DNS cache:")
	ui.Dim("  Chrome: chrome://net-internals/#dns  →  Clear host cache")
	ui.Dim("  Firefox: about:networking#dns  →  Clear DNS Cache")

	if err := traefik.CheckMkcert(); err != nil {
		ui.Warn("%v", err)
		ui.Dim("Local HTTPS will not work without mkcert")
		return
	}

	// Auto-install CA if not already installed
	if !traefik.IsCAInstalled() {
		ui.Dim("Installing mkcert CA...")
		res, err := traefik.InstallCA()
		if err != nil {
			ui.Warn("Failed to install mkcert CA: %v", err)
			ui.Dim("Local HTTPS may not work in browsers")
		} else {
			reportCAInstall(res, false)
		}
	}

	renewed, err := traefik.EnsureLocalCert(siteName, domains, wildcard)
	if err != nil {
		ui.Warn("Failed to generate certificate: %v", err)
		return
	}

	if renewed {
		ui.Dim("Generated SSL certificate for %s", primary)
		if err := traefik.UpdateDynamicConfig(); err != nil {
			ui.Warn("Failed to update Traefik config: %v", err)
		}
	}
}

// renewLocalCertIfNeeded checks if a local cert needs renewal and renews it.
// The cert is named after the primary (first) domain on disk.
func renewLocalCertIfNeeded(siteName string, domains []string, wildcard bool) {
	if len(domains) == 0 {
		return
	}
	primary := domains[0]
	cert := traefik.GetLocalCertInfo(siteName, primary)
	if !cert.Exists || cert.IsExpired || cert.DaysLeft <= traefik.RenewThresholdDays {
		if cert.IsExpired {
			ui.Dim("Renewing expired SSL certificate for %s...", primary)
		} else if cert.Exists && cert.DaysLeft <= traefik.RenewThresholdDays {
			ui.Dim("Renewing SSL certificate for %s (expires in %d days)...", primary, cert.DaysLeft)
		}

		if err := traefik.GenerateLocalCert(siteName, domains, wildcard); err != nil {
			ui.Warn("Failed to renew certificate: %v", err)
			return
		}

		if err := traefik.UpdateDynamicConfig(); err != nil {
			ui.Warn("Failed to update Traefik config: %v", err)
		}
	}
}

// startSiteAfterAdd starts the site after adding
func startSiteAfterAdd(cfg *config.Config, setup *siteSetup) error {
	ui.Blank()
	if setup.profile != "" {
		ui.Info("Starting site with profile '%s'...", setup.profile)
	} else {
		ui.Info("Starting site...")
	}

	// Determine the compose directory
	var composeDir string
	if setup.isStatic || setup.isNode || setup.isRuby || setup.isPython || setup.isDockerfile {
		// srv-managed sites have their compose file in the srv config directory
		composeDir = site.SiteConfigDir(cfg, setup.siteName)
	} else {
		// Compose sites run from the project directory
		composeDir = setup.sitePath
	}

	if err := docker.ComposeUpWithProfile(composeDir, setup.profile); err != nil {
		return fmt.Errorf("failed to start site: %w", err)
	}

	// For compose sites, connect service to traefik network.
	// srv-managed sites manage network membership via compose labels.
	if !setup.isStatic && !setup.isNode && !setup.isRuby && !setup.isPython && !setup.isDockerfile && setup.composeServiceName != "" {
		if err := docker.ConnectServiceToNetwork(setup.sitePath, setup.composeServiceName, cfg.NetworkName); err != nil {
			if errors.Is(err, docker.ErrServiceNotRunning) {
				ui.Dim("Service '%s' not running (may use Docker Compose profiles)", setup.composeServiceName)
				ui.Dim("Network connection will happen when you start with your profile")
			} else {
				ui.Warn("Could not connect to traefik network: %v", err)
				ui.Dim("Run manually: docker network connect %s <container_name>", cfg.NetworkName)
			}
		}
	}

	ui.Success("Site is running at https://%s", setup.domain)
	return nil
}
