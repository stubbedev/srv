// Package mcp — dryrun.go resolves what a destructive call would remove.
// dry_run used to return {ok:true} without even checking the target exists —
// an agent reasonably read that as "validated". Every preview resolves real
// state and lists the concrete effects, and a missing target is an error.
package mcp

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/proxy"
	"github.com/stubbedev/srv/internal/redirect"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/traefik"
)

// dryRunPreview is embedded in a destructive tool's output when dry_run was
// set: the resolved target and the effects the real call would perform.
type dryRunPreview struct {
	Target      string   `json:"target"`
	Kind        string   `json:"kind,omitempty"`
	WouldRemove []string `json:"would_remove"`
}

// siteMeta loads a site's metadata for a preview, erroring when the site is
// unknown so a dry_run cannot validate a typo.
func siteMeta(name string) (*site.SiteMetadata, error) {
	meta, err := site.ReadSiteMetadata(name)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, fmt.Errorf("site %q not found", name)
	}
	return meta, nil
}

// siteRemovalPreview lists what remove_site would stop and delete.
func siteRemovalPreview(name string) (*dryRunPreview, error) {
	meta, err := siteMeta(name)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	eff := []string{
		"containers stopped and removed",
		filepath.Join(cfg.SitesDir, name) + " (metadata)",
		traefik.SiteRouteConfigPath(cfg, name),
	}
	if meta.IsLocal {
		eff = append(eff,
			cfg.SiteCertsDir(name)+" (certificate)",
			"DNS registration: "+strings.Join(meta.Domains, ", "),
		)
	}
	return &dryRunPreview{Target: name, Kind: "site", WouldRemove: eff}, nil
}

// proxyRemovalPreview lists what remove_proxy would delete.
func proxyRemovalPreview(name string) (*dryRunPreview, error) {
	meta, err := proxy.Read(name)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, fmt.Errorf("proxy %q not found", name)
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	eff := []string{
		filepath.Join(cfg.TraefikConfDir(), constants.ProxyConfigPrefix+name+constants.ExtYAML),
	}
	if len(meta.Domains) > 0 {
		eff = append(eff,
			cfg.SiteCertsDir(proxy.CertSiteName(name))+" (certificate)",
			"DNS registration: "+strings.Join(meta.Domains, ", "),
		)
	}
	return &dryRunPreview{Target: name, Kind: "proxy", WouldRemove: eff}, nil
}

// redirectCertSiteName mirrors the redirect package's synthetic cert site.
func redirectCertSiteName(name string) string { return "_" + constants.RedirectConfigPrefix + name }

// redirectRemovalPreview lists what remove_redirect would delete.
func redirectRemovalPreview(name string) (*dryRunPreview, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	info := redirect.ReadInfo(cfg, name)
	if info.Domain == "" && !info.DNSOnly {
		return nil, fmt.Errorf("redirect %q not found", name)
	}
	eff := []string{filepath.Join(cfg.TraefikConfDir(), constants.RedirectConfigPrefix+name+constants.ExtYAML)}
	if info.DNSOnly {
		eff = append(eff, "dnsmasq alias for "+info.Domain)
	} else {
		eff = append(eff,
			cfg.SiteCertsDir(redirectCertSiteName(name))+" (certificate)",
			"DNS registration: "+info.Domain,
		)
	}
	return &dryRunPreview{Target: name, Kind: "redirect", WouldRemove: eff}, nil
}
