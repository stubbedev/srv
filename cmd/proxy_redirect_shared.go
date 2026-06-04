// Package cmd — proxy_redirect_shared.go holds the helpers that `srv proxy
// add` and `srv redirect add` (HTTP variant) both need: ensure mkcert + CA +
// cert for a (siteName, domain) pair, and render SSL status for list views.
// Both commands write a Traefik file-provider YAML — the storage shape is
// different but the cert lifecycle is identical.
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

// scanConfigNames lists the short names of every Traefik conf file matching
// prefix + <name> + .yml. Shared by `srv proxy list` and `srv redirect list`,
// which differ only in the filename prefix they scan for.
func scanConfigNames(prefix string) []string {
	cfg, err := config.Load()
	if err != nil {
		ui.VerboseLog("Warning: could not load config: %v", err)
		return nil
	}
	entries, err := os.ReadDir(cfg.TraefikConfDir())
	if err != nil {
		ui.VerboseLog("Warning: could not read traefik conf dir: %v", err)
		return nil
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, constants.ExtYAML) {
			names = append(names, strings.TrimSuffix(strings.TrimPrefix(name, prefix), constants.ExtYAML))
		}
	}
	return names
}

// ensureLocalCertForResource verifies mkcert is on $PATH, installs the local
// CA if needed, and issues / renews a cert for siteName + domain (+ wildcard).
// Used by proxy and redirect add — each picks a different siteName prefix so
// the cert files don't collide with real sites' certs.
func ensureLocalCertForResource(siteName, domain string, wildcard bool) error {
	if err := traefik.CheckMkcert(); err != nil {
		return err
	}
	if !traefik.IsCAInstalled() {
		ui.Dim("Installing mkcert CA...")
		res, err := traefik.InstallCA()
		if err != nil {
			return fmt.Errorf("failed to install mkcert CA: %w", err)
		}
		reportCAInstall(res, false)
	}
	renewed, err := traefik.EnsureLocalCert(siteName, []string{domain}, wildcard)
	if err != nil {
		return fmt.Errorf("failed to generate certificate: %w", err)
	}
	if renewed {
		ui.Dim("Generated SSL certificate for %s", domain)
	}
	return nil
}

// localCertStatus returns one of "corrupt" / "missing" / "expired" /
// "expiring" / "valid" for the cert pair (siteName, domain). Returns "" when
// domain is empty. Plain-text variant used by `--format json` outputs.
func localCertStatus(siteName, domain string) string {
	if domain == "" {
		return ""
	}
	return string(traefik.GetLocalCertInfo(siteName, domain).Status())
}

// localCertStatusColored wraps localCertStatus with ui.StatusColor / ui.DimText
// for the human-readable table outputs of `srv proxy list` / `srv redirect list`.
func localCertStatusColored(siteName, domain string) string {
	status := localCertStatus(siteName, domain)
	if status == "" {
		return ui.DimText("-")
	}
	return ui.StatusColor(status)
}
