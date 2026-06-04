// Package cmd — site_add_finalize.go retains the cert-renewal helper used by
// the start path. The add pipeline (detection, file generation, SSL, start)
// now lives in internal/site.Add, shared with the MCP add_site tool.
package cmd

import (
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

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
