// Package daemon — dns_zones.go feeds the embedded DNS server from the
// structured config instead of waiting on the rendered dnsmasq files: a
// dnsd.ZoneSnapshot is built from the local-domains registry, the DNS-alias
// redirect files, and config.yml's upstream list, and pushed into the
// running server in-process. The generated files remain loaded inside dnsd
// as the fallback layer — daemon start before the first snapshot lands, and
// hand-written entries during debugging.
package daemon

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/dnsd"
	"github.com/stubbedev/srv/internal/ops"
	"github.com/stubbedev/srv/internal/traefik"
)

// dnsZoneDebounce coalesces the event burst a registry save produces into
// one snapshot build.
const dnsZoneDebounce = 200 * time.Millisecond

// startDNSZoneSource watches the structured DNS inputs — the local-domains
// registry in the traefik dir, the redirect-*.yml alias files in its conf
// dir, and config.yml — and rebuilds the server's zone snapshot on every
// change, until the daemon stops. Best-effort: a watcher failure is logged
// and DNS keeps serving the last snapshot over the file fallback, converging
// on the next registry change or daemon restart.
func (d *Daemon) startDNSZoneSource() {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		d.log("DNS zone source unavailable (file changes apply on restart): %v", err)
		return
	}
	dirs := map[string]bool{}
	for _, dir := range []string{d.cfg.TraefikDir, d.cfg.TraefikConfDir(), d.cfg.Root} {
		if dir == "" {
			continue
		}
		dirs[dir] = true
	}
	for dir := range dirs {
		if err := w.Add(dir); err != nil {
			d.log("DNS zone source: watch %s: %v", dir, err)
		}
	}
	go d.dnsZoneLoop(w)
}

// isDNSInput reports whether a changed file is one of the structured DNS
// inputs. Everything else in the watched directories (Traefik dynamic
// config, site routes, logs) re-renders without touching the registry, so
// the snapshot must not rebuild for it.
func isDNSInput(path string) bool {
	base := filepath.Base(path)
	switch {
	case base == constants.LocalDomainsFile, base == constants.UserConfigFile:
		return true
	case strings.HasPrefix(base, constants.RedirectConfigPrefix) && strings.HasSuffix(base, constants.ExtYAML):
		return true
	default:
		return false
	}
}

// dnsZoneLoop drains watcher events and rebuilds the zone snapshot,
// debounced, until the daemon context is done.
func (d *Daemon) dnsZoneLoop(w *fsnotify.Watcher) {
	defer func() { _ = w.Close() }()

	debounce := time.NewTimer(dnsZoneDebounce)
	if !debounce.Stop() {
		<-debounce.C
	}
	pending := false
	for {
		select {
		case <-d.ctx.Done():
			return
		case event, ok := <-w.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 || !isDNSInput(event.Name) {
				continue
			}
			if !pending {
				pending = true
				debounce.Reset(dnsZoneDebounce)
			}
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			d.log("DNS zone source: watch error: %v", err)
		case <-debounce.C:
			if pending {
				pending = false
				d.refreshDNSZones()
			}
		}
	}
}

// refreshDNSZones builds a zone snapshot from the structured DNS inputs and
// pushes it into the running embedded server. On an input failure the
// previous snapshot stays installed: a broken or half-written input must not
// blank DNS, and the next change (or file fallback reload) converges.
func (d *Daemon) refreshDNSZones() {
	server := d.dns.Load()
	if server == nil {
		return
	}
	z := dnsd.NewZoneSnapshot()

	domains, err := traefik.LoadLocalDomains()
	if err != nil {
		d.log("DNS zone refresh: %v", err)
		return
	}
	for _, entry := range domains {
		if traefik.IsWildcardEntry(entry) {
			z.PinWildcard(traefik.BareDomain(entry), constants.LocalhostIP)
			continue
		}
		z.PinExact(entry, constants.LocalhostIP)
	}

	aliasDecls, err := traefik.ScanRedirectAliases()
	if err != nil {
		d.log("DNS zone refresh: scan redirect aliases: %v", err)
	} else {
		for _, a := range traefik.ResolveAliases(aliasDecls) {
			if a.ResolveErr != nil || a.IP == "" {
				d.log("DNS zone refresh: alias %s -> %s unresolved: %v", a.Source, a.Target, a.ResolveErr)
				continue
			}
			z.PinExact(a.Source, a.IP)
		}
	}

	if userCfg, err := ops.UserConfig(); err != nil {
		d.log("DNS zone refresh: user config: %v", err)
	} else {
		for _, spec := range userCfg.UpstreamDNS {
			z.SetUpstream(spec)
		}
	}

	server.SetZones(z)
}
