// Package proxy — orchestrate.go holds the headless add/remove flow shared by
// the `srv proxy` CLI and the MCP add_proxy/remove_proxy tools — including
// the `--fallback` sidecar. Keeping the core here means both surfaces
// validate, issue certs, register DNS, and write config identically.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/platform"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/validate"
)

// CertSiteName is the synthetic site name a proxy's local cert is stored
// under, kept distinct from real sites so cert files never collide.
func CertSiteName(name string) string { return "_proxy-" + name }

// AddSpec describes a proxy to create. Exactly one of Port or Container must
// be set. Container is "name:port". When FallbackURL is set, an nginx sidecar
// is placed in front of the primary upstream and 5xx responses transparently
// re-proxy to it.
type AddSpec struct {
	Name            string // optional; derived from Domain when empty
	Domain          string
	Port            string
	Container       string
	Wildcard        bool
	Force           bool
	FallbackURL     string // optional; e.g. https://prod.example.com
	FallbackTimeout string // optional connect timeout to the primary (default 2s)
}

// AddResult reports what Add produced.
type AddResult struct {
	Name      string `json:"name"`
	Domain    string `json:"domain"`
	TargetURL string `json:"target_url"`
	// FallbackEnabled is true when a fallback sidecar was placed in front of
	// the primary upstream; TargetURL then points at the sidecar.
	FallbackEnabled bool     `json:"fallback_enabled,omitempty"`
	Notes           []string `json:"notes,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
}

// Add validates the spec, issues a local cert, registers DNS, connects a
// container if needed, writes the Traefik config + metadata sidecar, and
// refreshes the dynamic config. Non-fatal steps (DNS, dynamic-config refresh)
// are collected as warnings rather than failing the whole operation.
func Add(cfg *config.Config, spec AddSpec) (*AddResult, error) {
	name, containerName, containerPort, isContainer, err := validateAddSpec(spec)
	if err != nil {
		return nil, err
	}

	proxyFile := filepath.Join(cfg.TraefikConfDir(), constants.ProxyConfigPrefix+name+constants.ExtYAML)
	if !spec.Force {
		if _, statErr := os.Stat(proxyFile); statErr == nil {
			return nil, fmt.Errorf("proxy %q already exists (set force to overwrite)", name)
		}
	}

	if _, err := traefik.EnsureResourceCert(CertSiteName(name), spec.Domain, spec.Wildcard); err != nil {
		return nil, err
	}

	res := &AddResult{Name: name, Domain: spec.Domain}

	if err := traefik.RegisterLocalDomain(spec.Domain, spec.Wildcard); err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("register DNS for %s: %v", spec.Domain, err))
	}

	targetURL, warn, err := resolveTarget(cfg, isContainer, containerName, containerPort, spec.Port)
	if err != nil {
		return nil, err
	}
	if warn != "" {
		res.Warnings = append(res.Warnings, warn)
	}
	if isContainer {
		res.Notes = append(res.Notes, fmt.Sprintf("Connected container '%s' to %s network", containerName, cfg.NetworkName))
	}

	// When a fallback is requested, sit the nginx sidecar in front of the
	// primary upstream so 5xx responses transparently re-proxy to it. Traefik
	// routes to the sidecar instead of the original target.
	if spec.FallbackURL != "" {
		sidecar := FallbackSpec{
			Name:            name,
			FallbackURL:     spec.FallbackURL,
			FallbackTimeout: spec.FallbackTimeout,
		}
		switch {
		case isContainer:
			// Bridge sidecar on the srv network; reaches the primary by name.
			sidecar.PrimaryHost = containerName
			sidecar.PrimaryPort = containerPort
		case platform.IsLinux():
			// Localhost-port primary on Linux: a bridge container cannot reach
			// a 127.0.0.1 host service, so the sidecar shares the host network
			// namespace and dials the loopback directly.
			sidecar.HostNetwork = true
			sidecar.PrimaryHost = constants.LocalhostIP
			sidecar.PrimaryPort = spec.Port
		default:
			// Docker Desktop: a bridge sidecar reaches the host via the alias.
			sidecar.PrimaryHost = constants.DockerHostInternal
			sidecar.PrimaryPort = spec.Port
		}
		sidecarURL, ferr := EnsureFallbackSidecar(cfg, sidecar)
		if ferr != nil {
			return nil, ferr
		}
		targetURL = sidecarURL
		res.FallbackEnabled = true
		res.Notes = append(res.Notes, fmt.Sprintf("Fallback sidecar started (%s)", FallbackContainerName(name)))
	}
	res.TargetURL = targetURL

	if err := traefik.WriteProxyConfig(cfg, traefik.ProxyRoute{
		Name:      name,
		Domain:    spec.Domain,
		TargetURL: targetURL,
		Container: containerName,
		Wildcard:  spec.Wildcard,
	}); err != nil {
		return nil, err
	}

	// Preserve any existing routes (and fallback record) when overwriting via Force.
	var existingRoutes []site.Route
	if pmeta, _ := Read(name); pmeta != nil {
		existingRoutes = pmeta.Routes
	}
	if err := Write(Metadata{
		Name:            name,
		Domains:         []string{spec.Domain},
		Wildcard:        spec.Wildcard,
		IsLocal:         true,
		Routes:          existingRoutes,
		FallbackURL:     spec.FallbackURL,
		FallbackTimeout: spec.FallbackTimeout,
	}); err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("write proxy metadata: %v", err))
	} else if len(existingRoutes) > 0 {
		if err := Reload(name); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("refresh proxy routes: %v", err))
		}
	}

	if err := traefik.UpdateDynamicConfig(); err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("update Traefik config: %v", err))
	}
	return res, nil
}

// RemoveProxy removes a proxy's Traefik config, local cert, DNS registration,
// routes config, fallback sidecar (when one was recorded or left behind by an
// older CLI-only add), and metadata sidecar, then refreshes the dynamic
// config. Returns an error only when the proxy does not exist; per-step
// failures are returned as warnings.
func RemoveProxy(cfg *config.Config, name string) (warnings []string, err error) {
	proxyFile := filepath.Join(cfg.TraefikConfDir(), constants.ProxyConfigPrefix+name+constants.ExtYAML)

	// Domain is needed to remove the matching cert + DNS registration. Prefer
	// the metadata sidecar; it is the canonical record of the proxy's domain.
	var domain string
	var hasFallback bool
	if pmeta, _ := Read(name); pmeta != nil {
		if len(pmeta.Domains) > 0 {
			domain = pmeta.Domains[0]
		}
		hasFallback = pmeta.FallbackURL != ""
	}
	// Older adds recorded the fallback only as a sidecar directory; tear that
	// down too so no proxy ever leaves its sidecar orphaned.
	if !hasFallback {
		if _, statErr := os.Stat(FallbackDir(cfg, name)); statErr == nil {
			hasFallback = true
		}
	}
	if hasFallback {
		if fbErr := RemoveFallbackSidecar(cfg, name); fbErr != nil {
			warnings = append(warnings, fmt.Sprintf("remove fallback sidecar: %v", fbErr))
		}
	}

	if rmErr := os.Remove(proxyFile); rmErr != nil {
		if os.IsNotExist(rmErr) {
			return nil, fmt.Errorf("proxy %q not found", name)
		}
		return nil, fmt.Errorf("remove proxy config: %w", rmErr)
	}

	if domain != "" {
		if err := traefik.RemoveLocalCerts(CertSiteName(name), domain); err != nil {
			warnings = append(warnings, fmt.Sprintf("remove certificate: %v", err))
		}
		if err := traefik.UnregisterLocalDomain(domain); err != nil {
			warnings = append(warnings, fmt.Sprintf("unregister DNS for %s: %v", domain, err))
		}
	}
	if err := traefik.RemoveRoutesConfig(cfg, name); err != nil {
		warnings = append(warnings, fmt.Sprintf("remove routes config: %v", err))
	}
	if err := Remove(name); err != nil {
		warnings = append(warnings, fmt.Sprintf("remove proxy metadata: %v", err))
	}
	if err := traefik.UpdateDynamicConfig(); err != nil {
		warnings = append(warnings, fmt.Sprintf("update Traefik config: %v", err))
	}
	return warnings, nil
}

// validateAddSpec mirrors the CLI's validateProxyInput: exactly one of
// port/container, valid domain/port/container, and a derived-or-validated name.
func validateAddSpec(spec AddSpec) (name, containerName, containerPort string, isContainer bool, err error) {
	if spec.Port == "" && spec.Container == "" {
		return "", "", "", false, errors.New("either port or container must be specified")
	}
	if spec.Port != "" && spec.Container != "" {
		return "", "", "", false, errors.New("port and container are mutually exclusive")
	}
	if err := validate.Domain(spec.Domain); err != nil {
		return "", "", "", false, fmt.Errorf("invalid domain: %w", err)
	}
	if spec.Container != "" {
		host, port, ok := splitContainer(spec.Container)
		if !ok {
			return "", "", "", false, errors.New("invalid container format, use name:port (e.g. myapp:3000)")
		}
		if err := validate.PortString(port); err != nil {
			return "", "", "", false, fmt.Errorf("invalid container port: %w", err)
		}
		if !docker.ContainerExists(host) {
			return "", "", "", false, fmt.Errorf("container %q does not exist", host)
		}
		containerName, containerPort, isContainer = host, port, true
	} else if err := validate.PortString(spec.Port); err != nil {
		return "", "", "", false, fmt.Errorf("invalid port: %w", err)
	}

	name = spec.Name
	if name == "" {
		name = site.SanitizeName(spec.Domain)
	}
	if err := validate.ProxyName(name); err != nil {
		return "", "", "", false, fmt.Errorf("invalid proxy name: %w", err)
	}
	return name, containerName, containerPort, isContainer, nil
}

// resolveTarget connects a container to the srv network (when applicable) and
// returns the upstream URL Traefik should route to. For a localhost-port
// target with nothing listening yet it returns a non-empty warning — users
// often register the proxy before starting their dev server, so it is not an
// error, but it must not be silent.
func resolveTarget(cfg *config.Config, isContainer bool, containerName, containerPort, port string) (url string, warn string, err error) {
	if !isContainer {
		// Best-effort liveness check; not fatal — proxies are often added before
		// the dev server starts.
		dialer := &net.Dialer{Timeout: 500 * time.Millisecond}
		if conn, dialErr := dialer.DialContext(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", port)); dialErr == nil {
			_ = conn.Close()
		} else {
			warn = fmt.Sprintf("nothing is listening on port %s — start your service before using the proxy", port)
		}
		// On Linux, Traefik uses network_mode: host, so it can reach localhost
		// directly. Use "localhost" rather than "127.0.0.1" so that services
		// bound only to the IPv6 loopback (::1) — e.g. Nuxt, Vite — are also
		// reachable. On Mac/Windows, Traefik runs in bridge mode and needs
		// host.docker.internal.
		host := constants.DockerHostInternal
		if platform.IsLinux() {
			host = constants.LocalhostAlias
		}
		return fmt.Sprintf("http://%s:%s", host, port), warn, nil
	}
	if err := docker.CreateNetwork(cfg.NetworkName); err != nil {
		return "", "", fmt.Errorf("create network: %w", err)
	}
	if err := docker.ConnectContainerToNetwork(containerName, cfg.NetworkName, ""); err != nil {
		return "", "", fmt.Errorf("connect container to network: %w", err)
	}
	return fmt.Sprintf("http://%s:%s", containerName, containerPort), "", nil
}

func splitContainer(s string) (host, port string, ok bool) {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			return s[:i], s[i+1:], i > 0 && i < len(s)-1
		}
	}
	return "", "", false
}
