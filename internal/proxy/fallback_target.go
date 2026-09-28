// Package proxy — fallback_target.go keeps a --fallback from pointing back at
// Traefik itself. The usual shape is `--domain myapp.com --fallback
// https://myapp.com`: the hostname srv now answers locally. Traefik resolves
// through the system resolver, i.e. srv's own DNS, so the fallback would dial
// Traefik, match this very router, fail the primary again and fail over again
// — a request loop that never answers. For such hosts the fallback is rendered
// as the address srv's DNS would forward to, with the hostname carried as the
// TLS server name.
package proxy

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"sync"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/dnsd"
	"github.com/stubbedev/srv/internal/platform"
	"github.com/stubbedev/srv/internal/traefik"
)

// dnsView is what fallback rendering needs from srv's DNS: whether it answers
// a name itself, and that name's address past it. *dnsd.ZoneSnapshot.
type dnsView interface {
	Owns(name string) bool
	ResolveA(name string) (net.IP, error)
}

// localZone is srv's DNS as Traefik sees it for local domains: the registry
// plus the configured upstreams. A config.yml that fails to load leaves the
// dnsd defaults in place, as the DNS server's file layer does.
func localZone() (dnsView, error) {
	z := dnsd.NewZoneSnapshot()
	if err := traefik.PinLocalDomains(z); err != nil {
		return nil, err
	}
	_ = traefik.SetConfiguredUpstreams(z)
	return z, nil
}

// localPrimaryURL is the upstream URL for a service on a host port: Linux
// Traefik runs with network_mode: host and reaches it on localhost (not
// 127.0.0.1, so services bound only to ::1 — e.g. Nuxt, Vite — work too);
// Mac/Windows Traefik runs in bridge mode and needs host.docker.internal.
func localPrimaryURL(port string) string {
	host := constants.DockerHostInternal
	if platform.IsLinux() {
		host = constants.LocalhostAlias
	}
	return "http://" + net.JoinHostPort(host, port)
}

// proxyRoute is the proxy-<name>.yml input for meta forwarding to primaryURL.
// dns is consulted only when meta has a fallback.
func proxyRoute(dns func() (dnsView, error), meta *Metadata, primaryURL string) (traefik.ProxyRoute, error) {
	p := traefik.ProxyRoute{
		Name:         meta.Name,
		Domain:       firstDomain(meta),
		TargetURL:    primaryURL,
		Wildcard:     meta.Wildcard,
		FallbackURL:  meta.FallbackURL,
		FallbackDial: meta.FallbackTimeout,
	}
	if meta.FallbackURL == "" {
		return p, nil
	}
	view, err := dns()
	if err != nil {
		return p, fmt.Errorf("read local DNS registry: %w", err)
	}
	p.FallbackURL, p.FallbackServerName, err = fallbackTarget(view, meta.FallbackURL)
	return p, err
}

// fallbackTarget returns the URL Traefik should dial for raw and the TLS
// server name to present. A host srv's DNS does not answer is returned
// unchanged with no server name.
func fallbackTarget(dns dnsView, raw string) (dialURL, serverName string, err error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("invalid fallback url: %w", err)
	}
	host := u.Hostname()
	if net.ParseIP(host) != nil || !dns.Owns(host) {
		return raw, "", nil
	}
	ip, err := dns.ResolveA(host)
	if err != nil {
		return "", "", fmt.Errorf("fallback host %s is served by srv itself, so Traefik would fail over to itself; resolving its real address upstream failed: %w", host, err)
	}
	if port := u.Port(); port != "" {
		u.Host = net.JoinHostPort(ip.String(), port)
	} else {
		u.Host = ip.String()
	}
	return u.String(), host, nil
}

// ReconcileFallbacks brings every proxy's fallback rendering up to date: a
// retired failover hop is migrated to Traefik's native failover, and a
// fallback whose host srv shadows is re-pinned to its current upstream
// address. Never fails: per-proxy problems come back as warnings.
func ReconcileFallbacks(cfg *config.Config) []string {
	entries, err := os.ReadDir(proxiesDir(cfg))
	if err != nil {
		return nil // no proxies dir yet — nothing to reconcile
	}
	dns := sync.OnceValues(localZone)
	var warn []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		meta, err := Read(e.Name())
		if err != nil || meta == nil || (meta.FallbackURL == "" && meta.FallbackPort == 0) {
			continue
		}
		if legacyFallback(cfg, meta) {
			warn = append(warn, migrateLegacyFallback(cfg, dns, meta)...)
			continue
		}
		if err := repinFallback(cfg, dns, meta); err != nil {
			warn = append(warn, fmt.Sprintf("proxy %s: %v", meta.Name, err))
		}
	}
	return warn
}

// repinFallback re-renders a localhost-primary proxy whose fallback host srv
// shadows. Any other proxy is left alone: its rendering does not depend on
// DNS, and a container primary's target only `srv proxy add` can recover.
func repinFallback(cfg *config.Config, dns func() (dnsView, error), meta *Metadata) error {
	if meta.Port <= 0 {
		return nil
	}
	route, err := proxyRoute(dns, meta, localPrimaryURL(strconv.Itoa(meta.Port)))
	if err != nil || route.FallbackServerName == "" {
		return err
	}
	return traefik.WriteProxyConfig(cfg, route)
}
