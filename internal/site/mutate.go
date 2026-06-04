// Package site — mutate.go holds headless metadata mutators (aliases, the
// internal listener, volumes) shared by the `srv alias|internal|volume` CLI and
// the MCP tools. Each reads metadata, edits it, writes it back, syncs the
// derived DNS/cert/routing state, and returns non-fatal issues as warnings.
package site

import (
	"fmt"
	"strings"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/validate"
)

// regenerateRouting rewrites a compose site's Traefik route config from its
// current metadata. No-op for non-compose sites (static/dockerfile route config
// is produced by Reload).
func regenerateRouting(siteName string, meta *SiteMetadata) error {
	if meta.Type != SiteTypeCompose {
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return traefik.WriteSiteRouteConfig(cfg, traefik.SiteRouteConfig{
		Name:        siteName,
		Domains:     meta.Domains,
		ServiceName: meta.ServiceName,
		Port:        meta.Port,
		IsLocal:     meta.IsLocal,
		Wildcard:    meta.Wildcard,
		Listeners:   meta.Listeners,
	})
}

// refreshLocalCert re-issues a local site's cert to cover its current domain
// set and refreshes the Traefik dynamic config. Best-effort: returns warnings
// rather than failing the mutation. Does not install the CA (a site that is
// local already has one); a missing CA surfaces as a warning.
func refreshLocalCert(siteName string, meta *SiteMetadata) (warnings []string) {
	for _, d := range meta.Domains {
		if err := traefik.RegisterLocalDomain(d, meta.Wildcard); err != nil {
			warnings = append(warnings, fmt.Sprintf("register DNS for %s: %v", d, err))
		}
	}
	if renewed, err := traefik.EnsureLocalCert(siteName, meta.Domains, meta.Wildcard); err != nil {
		warnings = append(warnings, fmt.Sprintf("refresh certificate: %v", err))
	} else if renewed {
		if err := traefik.UpdateDynamicConfig(); err != nil {
			warnings = append(warnings, fmt.Sprintf("update Traefik config: %v", err))
		}
	}
	return warnings
}

// requireMeta loads a site's metadata, erroring if the site is unknown.
func requireMeta(siteName string) (*SiteMetadata, error) {
	meta, err := ReadSiteMetadata(siteName)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, fmt.Errorf("site %q not found", siteName)
	}
	return meta, nil
}

// AddAlias adds an extra hostname to a site. Returns changed=false (no error)
// when the alias is already present.
func AddAlias(siteName, alias string) (changed bool, warnings []string, err error) {
	alias = strings.ToLower(strings.TrimSpace(alias))
	if err := validate.Domain(alias); err != nil {
		return false, nil, fmt.Errorf("invalid alias: %w", err)
	}
	meta, err := requireMeta(siteName)
	if err != nil {
		return false, nil, err
	}
	if len(meta.Domains) == 0 {
		return false, nil, fmt.Errorf("site %q has no canonical domain", siteName)
	}
	for _, d := range meta.Domains {
		if d == alias {
			return false, nil, nil
		}
	}
	meta.Domains = append(meta.Domains, alias)
	if err := WriteSiteMetadata(siteName, *meta); err != nil {
		return false, nil, fmt.Errorf("update site metadata: %w", err)
	}
	if meta.IsLocal {
		warnings = append(warnings, refreshLocalCert(siteName, meta)...)
	}
	if err := regenerateRouting(siteName, meta); err != nil {
		warnings = append(warnings, fmt.Sprintf("refresh routing config: %v", err))
	}
	return true, warnings, nil
}

// RemoveAlias drops an extra hostname from a site. The canonical (first) domain
// cannot be removed this way.
func RemoveAlias(siteName, alias string) (warnings []string, err error) {
	alias = strings.ToLower(strings.TrimSpace(alias))
	meta, err := requireMeta(siteName)
	if err != nil {
		return nil, err
	}
	if len(meta.Domains) > 0 && meta.Domains[0] == alias {
		return nil, fmt.Errorf("%s is the canonical domain — remove the site to drop it", alias)
	}
	filtered := meta.Domains[:0]
	removed := false
	for _, d := range meta.Domains {
		if d == alias {
			removed = true
			continue
		}
		filtered = append(filtered, d)
	}
	if !removed {
		return nil, fmt.Errorf("alias %q is not registered for %s", alias, siteName)
	}
	meta.Domains = filtered
	if err := WriteSiteMetadata(siteName, *meta); err != nil {
		return nil, fmt.Errorf("update site metadata: %w", err)
	}
	if meta.IsLocal {
		if err := traefik.UnregisterLocalDomain(alias); err != nil {
			warnings = append(warnings, fmt.Sprintf("unregister DNS for %s: %v", alias, err))
		}
		warnings = append(warnings, refreshLocalCert(siteName, meta)...)
	}
	if err := regenerateRouting(siteName, meta); err != nil {
		warnings = append(warnings, fmt.Sprintf("refresh routing config: %v", err))
	}
	return warnings, nil
}

// SetInternalListener enables or disables the plain-HTTP `internal` entrypoint
// for a site. Returns changed=false when already in the requested state.
func SetInternalListener(siteName string, enable bool) (changed bool, warnings []string, err error) {
	meta, err := requireMeta(siteName)
	if err != nil {
		return false, nil, err
	}
	has := HasListener(meta.Listeners, constants.ListenerInternal)
	if has == enable {
		return false, nil, nil
	}
	if enable {
		meta.Listeners = append(meta.Listeners, constants.ListenerInternal)
	} else {
		filtered := meta.Listeners[:0]
		for _, l := range meta.Listeners {
			if l != constants.ListenerInternal {
				filtered = append(filtered, l)
			}
		}
		meta.Listeners = filtered
	}
	if err := WriteSiteMetadata(siteName, *meta); err != nil {
		return false, nil, fmt.Errorf("update site metadata: %w", err)
	}
	if err := regenerateRouting(siteName, meta); err != nil {
		warnings = append(warnings, fmt.Sprintf("refresh routing config: %v", err))
	}
	return true, warnings, nil
}

// AddVolume attaches an extra bind-mount to a site's container. Rejects a target
// that collides with an existing mount or overlaps the project bind at /app.
func AddVolume(siteName string, mount VolumeMount) (warnings []string, err error) {
	meta, err := requireMeta(siteName)
	if err != nil {
		return nil, err
	}
	for _, existing := range meta.Volumes {
		if existing.Target == mount.Target {
			return nil, fmt.Errorf("a volume with target %q is already attached — remove it first", mount.Target)
		}
	}
	if mount.Target == "/app" || strings.HasPrefix(mount.Target, "/app/") {
		return nil, fmt.Errorf("target %q overlaps the project bind at /app — pick a different container path", mount.Target)
	}
	meta.Volumes = append(meta.Volumes, mount)
	if err := WriteSiteMetadata(siteName, *meta); err != nil {
		return nil, fmt.Errorf("write metadata: %w", err)
	}
	if _, err := Reload(siteName); err != nil {
		warnings = append(warnings, fmt.Sprintf("refresh site config: %v", err))
	}
	return warnings, nil
}

// RemoveVolume detaches a bind-mount by container target path.
func RemoveVolume(siteName, target string) (warnings []string, err error) {
	meta, err := requireMeta(siteName)
	if err != nil {
		return nil, err
	}
	filtered := meta.Volumes[:0]
	removed := false
	for _, v := range meta.Volumes {
		if v.Target == target {
			removed = true
			continue
		}
		filtered = append(filtered, v)
	}
	if !removed {
		return nil, fmt.Errorf("no volume with target %q attached to %s", target, siteName)
	}
	meta.Volumes = filtered
	if err := WriteSiteMetadata(siteName, *meta); err != nil {
		return nil, fmt.Errorf("write metadata: %w", err)
	}
	if _, err := Reload(siteName); err != nil {
		warnings = append(warnings, fmt.Sprintf("refresh site config: %v", err))
	}
	return warnings, nil
}
