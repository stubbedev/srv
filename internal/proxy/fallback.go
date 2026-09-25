// Package proxy — fallback.go holds everything around `srv proxy add
// --fallback` other than the route rendering: URL validation, the Traefik
// version guard, teardown of the retired failover hops (the daemon-hosted
// listener and the nginx sidecar), and the one-shot migration that re-renders
// proxies created before the hop was removed. The failover itself is Traefik's
// native `failover` service, rendered by traefik.WriteProxyConfig.
package proxy

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/traefik"
)

// fallbackContainerName is the container name of the retired nginx sidecar.
// Kept only so upgrades can find and remove it.
func fallbackContainerName(name string) string {
	return "srv-proxy-" + name + "-fallback"
}

// FallbackDir returns the directory where the retired sidecar's generated
// docker-compose.yml and nginx.conf lived. It is still consulted during
// migration (to recover the primary upstream from proxy_pass) and teardown.
func FallbackDir(cfg *config.Config, name string) string {
	return filepath.Join(cfg.SitesDir, "_proxy-"+name+"-fallback")
}

// validateFallbackURL enforces the fallback contract at the boundary: an
// absolute http(s) URL, since it becomes a Traefik load-balancer server.
func validateFallbackURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid fallback url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("fallback url must be an absolute http:// or https:// URL")
	}
	if u.Host == "" {
		return errors.New("fallback url must include a host")
	}
	return nil
}

// ensureTraefikSupportsFailover checks the running Traefik container serves
// failover.errors (added in v3.7). srv pins traefik:v3.7, so this only trips
// on an install whose Traefik predates the pin and has not been updated.
func ensureTraefikSupportsFailover() error {
	v := docker.GetContainerImageVersion(docker.ContainerTraefik)
	major, minor, ok := parseSemverMinor(v)
	if !ok {
		// Version undetectable: don't block — the pinned image guarantees the
		// feature, and the route file is re-renderable if Traefik rejects it.
		return nil
	}
	if major < 3 || (major == 3 && minor < 7) {
		return fmt.Errorf("traefik %s has no failover.errors support (needs >= 3.7) — run 'srv update' to pull the pinned image", v)
	}
	return nil
}

// parseSemverMinor extracts major.minor from strings like "3.7.9", "v3.7.9",
// or "traefik:v3.7.9". Returns ok=false when nothing parseable is found.
func parseSemverMinor(v string) (major, minor int, ok bool) {
	if i := strings.LastIndex(v, ":"); i >= 0 {
		v = v[i+1:]
	}
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return major, minor, true
}

// proxyPassRe extracts the primary upstream from the retired sidecar's nginx
// conf: `proxy_pass http://<host>:<port>;`.
var proxyPassRe = regexp.MustCompile(`proxy_pass\s+http://([^/:\s]+):(\d+)\s*;`)

// legacyFallback reports whether the proxy still carries a retired hop: a
// daemon-hosted listener port in its metadata, or an nginx sidecar on disk.
func legacyFallback(cfg *config.Config, meta *Metadata) bool {
	if meta != nil && meta.FallbackPort > 0 {
		return true
	}
	if meta == nil || meta.FallbackURL == "" {
		return false
	}
	_, err := os.Stat(FallbackDir(cfg, meta.Name))
	return err == nil
}

// MigrateLegacyFallback re-renders a proxy whose fallback predates the native
// Traefik failover: the daemon-hosted listener (localhost primaries) or the
// nginx sidecar (container primaries) is replaced by the failover service,
// and the retired hop is removed. Returns warnings for anything it had to
// skip; a proxy without a legacy hop is a silent no-op.
func MigrateLegacyFallback(cfg *config.Config, meta *Metadata) []string {
	if !legacyFallback(cfg, meta) {
		return nil
	}
	name := meta.Name
	var warn []string

	// Recover the primary upstream URL the way Add would have rendered it.
	// The two retired hops hid the real primary behind themselves: the
	// listener dialed 127.0.0.1:<meta.Port>, the sidecar's nginx.conf holds
	// its proxy_pass target verbatim.
	var primaryURL string
	switch {
	case meta.Port > 0:
		primaryURL = fmt.Sprintf("http://%s:%d", constants.LocalhostAlias, meta.Port)
	default:
		data, err := os.ReadFile(filepath.Join(FallbackDir(cfg, name), "nginx.conf"))
		if err != nil {
			return append(warn, fmt.Sprintf("proxy %s: cannot recover its primary upstream from the retired sidecar (%v); re-run 'srv proxy add --force' to rebuild it", name, err))
		}
		m := proxyPassRe.FindStringSubmatch(string(data))
		if m == nil {
			return append(warn, fmt.Sprintf("proxy %s: no proxy_pass in the retired sidecar config; re-run 'srv proxy add --force' to rebuild it", name))
		}
		host, port := m[1], m[2]
		if host != constants.LocalhostIP && host != "127.0.0.1" {
			primaryURL = fmt.Sprintf("http://%s:%s", host, port)
		} else {
			primaryURL = fmt.Sprintf("http://%s:%s", constants.LocalhostAlias, port)
		}
	}

	if err := traefik.WriteProxyConfig(cfg, traefik.ProxyRoute{
		Name:         name,
		Domain:       firstDomain(meta),
		TargetURL:    primaryURL,
		Wildcard:     meta.Wildcard,
		FallbackURL:  meta.FallbackURL,
		FallbackDial: meta.FallbackTimeout,
	}); err != nil {
		return append(warn, fmt.Sprintf("proxy %s: re-render native failover: %v", name, err))
	}

	// Clear the retired listener port so this stays a one-shot migration even
	// if the sidecar teardown below fails.
	updated := *meta
	updated.FallbackPort = 0
	if err := Write(updated); err != nil {
		warn = append(warn, fmt.Sprintf("proxy %s: update metadata: %v", name, err))
	}

	if err := RemoveLegacySidecar(cfg, name); err != nil {
		warn = append(warn, fmt.Sprintf("proxy %s: remove retired sidecar: %v", name, err))
	}
	return warn
}

// RemoveLegacySidecar stops the retired nginx sidecar container and deletes
// its directory. A missing sidecar is a no-op. Stop failures are warned on
// stderr: the directory is still removed, since leaving it behind after a
// partial cleanup is worse than a stranded container the user can `docker rm`.
func RemoveLegacySidecar(cfg *config.Config, name string) error {
	dir := FallbackDir(cfg, name)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := docker.ComposeDown(dir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not stop retired fallback sidecar %s: %v\n", fallbackContainerName(name), err)
	}
	return os.RemoveAll(dir)
}

func firstDomain(meta *Metadata) string {
	if len(meta.Domains) == 0 {
		return ""
	}
	return meta.Domains[0]
}
