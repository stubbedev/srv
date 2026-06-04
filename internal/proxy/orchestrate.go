// Package proxy — orchestrate.go holds the headless add/remove flow shared by
// the `srv proxy` CLI and the MCP add_proxy/remove_proxy tools. It does NOT
// cover the CLI-only `--fallback` sidecar; callers that need that compose it on
// top (see cmd/proxy.go). Keeping the core here means both surfaces validate,
// issue certs, register DNS, and write config identically.
package proxy

import (
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

// certSiteName is the synthetic site name a proxy's local cert is stored under,
// kept distinct from real sites so cert files never collide.
func certSiteName(name string) string { return "_proxy-" + name }

// AddSpec describes a proxy to create. Exactly one of Port or Container must be
// set. Container is "name:port".
type AddSpec struct {
	Name      string // optional; derived from Domain when empty
	Domain    string
	Port      string
	Container string
	Wildcard  bool
	Force     bool
}

// AddResult reports what Add produced.
type AddResult struct {
	Name      string   `json:"name"`
	Domain    string   `json:"domain"`
	TargetURL string   `json:"target_url"`
	Warnings  []string `json:"warnings,omitempty"`
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

	if _, err := traefik.EnsureResourceCert(certSiteName(name), spec.Domain, spec.Wildcard); err != nil {
		return nil, err
	}

	res := &AddResult{Name: name, Domain: spec.Domain}

	if err := traefik.RegisterLocalDomain(spec.Domain, spec.Wildcard); err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("register DNS for %s: %v", spec.Domain, err))
	}

	targetURL, err := resolveTarget(cfg, isContainer, containerName, containerPort, spec.Port)
	if err != nil {
		return nil, err
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

	// Preserve any existing routes when overwriting via Force.
	var existingRoutes []site.Route
	if pmeta, _ := Read(name); pmeta != nil {
		existingRoutes = pmeta.Routes
	}
	if err := Write(Metadata{
		Name:     name,
		Domains:  []string{spec.Domain},
		Wildcard: spec.Wildcard,
		IsLocal:  true,
		Routes:   existingRoutes,
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
// routes config, and metadata sidecar, then refreshes the dynamic config. It
// does NOT tear down a `--fallback` sidecar (CLI-only); cmd handles that.
// Returns an error only when the proxy does not exist; per-step failures are
// returned as warnings.
func RemoveProxy(cfg *config.Config, name string) (warnings []string, err error) {
	proxyFile := filepath.Join(cfg.TraefikConfDir(), constants.ProxyConfigPrefix+name+constants.ExtYAML)

	// Domain is needed to remove the matching cert + DNS registration. Prefer
	// the metadata sidecar; it is the canonical record of the proxy's domain.
	var domain string
	if pmeta, _ := Read(name); pmeta != nil && len(pmeta.Domains) > 0 {
		domain = pmeta.Domains[0]
	}

	if rmErr := os.Remove(proxyFile); rmErr != nil {
		if os.IsNotExist(rmErr) {
			return nil, fmt.Errorf("proxy %q not found", name)
		}
		return nil, fmt.Errorf("remove proxy config: %w", rmErr)
	}

	if domain != "" {
		if err := traefik.RemoveLocalCerts(certSiteName(name), domain); err != nil {
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
		return "", "", "", false, fmt.Errorf("either port or container must be specified")
	}
	if spec.Port != "" && spec.Container != "" {
		return "", "", "", false, fmt.Errorf("port and container are mutually exclusive")
	}
	if err := validate.Domain(spec.Domain); err != nil {
		return "", "", "", false, fmt.Errorf("invalid domain: %w", err)
	}
	if spec.Container != "" {
		host, port, ok := splitContainer(spec.Container)
		if !ok {
			return "", "", "", false, fmt.Errorf("invalid container format, use name:port (e.g. myapp:3000)")
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
// returns the upstream URL Traefik should route to.
func resolveTarget(cfg *config.Config, isContainer bool, containerName, containerPort, port string) (string, error) {
	if !isContainer {
		// Best-effort liveness check; not fatal — proxies are often added before
		// the dev server starts.
		if conn, dialErr := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), 500*time.Millisecond); dialErr == nil {
			_ = conn.Close()
		}
		host := constants.DockerHostInternal
		if platform.IsLinux() {
			host = constants.LocalhostAlias
		}
		return fmt.Sprintf("http://%s:%s", host, port), nil
	}
	if err := docker.CreateNetwork(cfg.NetworkName); err != nil {
		return "", fmt.Errorf("create network: %w", err)
	}
	if err := docker.ConnectContainerToNetwork(containerName, cfg.NetworkName, ""); err != nil {
		return "", fmt.Errorf("connect container to network: %w", err)
	}
	return fmt.Sprintf("http://%s:%s", containerName, containerPort), nil
}

func splitContainer(s string) (host, port string, ok bool) {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			return s[:i], s[i+1:], i > 0 && i < len(s)-1
		}
	}
	return "", "", false
}
