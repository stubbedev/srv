package cmd

import (
	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

// =============================================================================
// trust command
// =============================================================================

var trustFlags struct {
	force bool
}

var trustCmd = &cobra.Command{
	Use:   "trust",
	Short: "Install local CA",
	Long: `Install the mkcert CA certificate and show status of local SSL certificates.

Certificates are automatically generated for each .test/.local/.localhost domain
when you add a site with 'srv add'.

Requires mkcert to be installed:
  - macOS: brew install mkcert
  - Linux: https://github.com/FiloSottile/mkcert#linux

Use --force to regenerate all certificates.`,
	RunE: runTrust,
}

func init() {
	trustCmd.Flags().BoolVarP(&trustFlags.force, "force", "f", false, "Force regenerate all certificates")
	RootCmd.AddCommand(trustCmd)
}

func runTrust(cmd *cobra.Command, args []string) error {
	// Check mkcert is installed
	if err := traefik.CheckMkcert(); err != nil {
		return err
	}

	// Check CA status
	caInstalled := traefik.IsCAInstalled()
	if caInstalled {
		ui.Success("mkcert CA is installed")
	} else {
		ui.Info("Installing mkcert CA...")
		if err := traefik.InstallCA(); err != nil {
			return err
		}
		ui.Success("mkcert CA installed")
		ui.Blank()
		ui.Warn("Restart your browser for the CA to take effect")
	}

	ui.Blank()

	// List all local certificates
	certs := traefik.ListLocalCerts()
	if len(certs) == 0 {
		ui.Dim("No local SSL certificates")
		ui.Dim("Certificates are generated when adding .test/.local sites")
		return nil
	}

	ui.Info("Local SSL certificates:")
	regenerated := false
	for _, cert := range certs {
		if trustFlags.force {
			// Regenerate certificate
			ui.IndentedDim(1, "Regenerating %s...", cert.Domain)
			if err := traefik.GenerateLocalCert(cert.Domain); err != nil {
				ui.IndentedError(1, "Failed to regenerate %s: %v", cert.Domain, err)
			} else {
				ui.IndentedSuccess(1, "%s - regenerated", cert.Domain)
				regenerated = true
			}
		} else if cert.IsExpired {
			ui.IndentedError(1, "%s - EXPIRED", cert.Domain)
		} else if cert.DaysLeft <= 30 {
			ui.IndentedWarn(1, "%s - expires in %d days", cert.Domain, cert.DaysLeft)
		} else {
			ui.IndentedSuccess(1, "%s - valid (%d days)", cert.Domain, cert.DaysLeft)
		}
	}

	if regenerated {
		// Update dynamic config
		if err := traefik.UpdateDynamicConfig(); err != nil {
			ui.Warn("Warning: Failed to update Traefik config: %v", err)
		}

		// Restart Traefik if running
		if traefik.IsRunning() {
			ui.Blank()
			ui.Info("Restarting Traefik to load new certificates...")
			cfg, err := config.Load()
			if err == nil {
				if err := docker.ComposeRestart(cfg.TraefikDir); err != nil {
					ui.Warn("Warning: Failed to restart Traefik: %v", err)
				}
			}
		}
	}

	return nil
}
