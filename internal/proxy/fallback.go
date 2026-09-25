// Package proxy — fallback.go renders and manages the nginx sidecar used by
// `srv proxy add --fallback`. The sidecar fronts the primary upstream and
// transparently re-proxies to a remote URL when the primary returns 5xx.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/fallbackd"
	"github.com/stubbedev/srv/internal/nginx"
)

// FallbackSpec captures what the sidecar needs to know to generate its config.
type FallbackSpec struct {
	Name            string // proxy name (used in container + dir names)
	PrimaryHost     string // host the sidecar dials for the primary upstream
	PrimaryPort     string
	FallbackURL     string // e.g. https://myapp.com
	FallbackTimeout string // e.g. 2s
	// HostNetwork runs the sidecar in the host network namespace so it can
	// reach a primary upstream bound to 127.0.0.1. Required on Linux for a
	// localhost-port proxy: a bridge container cannot reach host loopback
	// services, and the host firewall blocks bridge->host traffic.
	HostNetwork bool
	// ListenPort is the loopback port the sidecar's nginx listens on when
	// HostNetwork is set — it cannot use :80, which Traefik owns. Ignored for
	// a bridge sidecar, which always listens on :80.
	ListenPort int
}

// FallbackContainerName returns the container name for a fallback sidecar.
func FallbackContainerName(name string) string {
	return "srv-proxy-" + name + "-fallback"
}

// FallbackDir returns the directory for a fallback sidecar's generated
// docker-compose.yml and nginx.conf.
func FallbackDir(cfg *config.Config, name string) string {
	return filepath.Join(cfg.SitesDir, "_proxy-"+name+"-fallback")
}

// findFreeLoopbackPort asks the OS for an unused TCP port on 127.0.0.1 by
// binding port 0 and reading back the assignment. There is an unavoidable
// race between releasing the port and the sidecar binding it, but the window
// is tiny and the sidecar starts immediately after.
func findFreeLoopbackPort() (int, error) {
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocate loopback port: %w", err)
	}
	defer func() { _ = l.Close() }()
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected listener address type %T", l.Addr())
	}
	return addr.Port, nil
}

// existingOrNewFallbackPort returns the daemon-hosted fallback port recorded
// in the proxy's metadata, allocating a fresh one when the proxy is new (or
// predates persisted ports). Reusing the recorded port on a --force
// regeneration keeps the Traefik route's target URL stable.
func existingOrNewFallbackPort(name string) (int, error) {
	if pmeta, _ := Read(name); pmeta != nil && pmeta.FallbackPort > 0 {
		return pmeta.FallbackPort, nil
	}
	return fallbackd.AllocatePort()
}

// EnsureFallbackSidecar renders the nginx.conf + docker-compose.yml for a
// fallback sidecar and starts (or force-recreates) the container. Returns the
// URL Traefik should route to.
func EnsureFallbackSidecar(cfg *config.Config, spec FallbackSpec) (string, error) {
	if spec.HostNetwork {
		port, err := findFreeLoopbackPort()
		if err != nil {
			return "", err
		}
		spec.ListenPort = port
	}

	dir := FallbackDir(cfg, spec.Name)
	if err := os.MkdirAll(dir, constants.DirPermDefault); err != nil {
		return "", fmt.Errorf("create fallback dir: %w", err)
	}

	nginxConf, err := renderFallbackNginx(spec)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "nginx.conf"), []byte(nginxConf), constants.FilePermDefault); err != nil {
		return "", fmt.Errorf("write fallback nginx.conf: %w", err)
	}

	compose := renderFallbackCompose(spec, dir, cfg.NetworkName)
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(compose), constants.FilePermDefault); err != nil {
		return "", fmt.Errorf("write fallback compose: %w", err)
	}

	// Force-recreate: nginx.conf is bind-mounted, so a config-only change does
	// not alter the compose spec and a plain `up -d` would leave the sidecar
	// running its old (possibly looping) config after a regenerate/upgrade.
	if err := docker.ComposeUpForceRecreate(dir); err != nil {
		return "", fmt.Errorf("start fallback sidecar: %w", err)
	}

	// A host-network sidecar is reached on the loopback port it binds; a
	// bridge sidecar is reached by container name on the srv network.
	if spec.HostNetwork {
		return fmt.Sprintf("http://127.0.0.1:%d", spec.ListenPort), nil
	}
	return fmt.Sprintf("http://%s:80", FallbackContainerName(spec.Name)), nil
}

// RemoveFallbackSidecar stops the sidecar container and deletes its directory.
// A missing sidecar is a no-op. Stop failures are warned on stderr: the
// compose directory is still removed, since leaving it behind after a partial
// cleanup is worse than a stranded container the user can `docker rm`.
func RemoveFallbackSidecar(cfg *config.Config, name string) error {
	dir := FallbackDir(cfg, name)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := docker.ComposeDown(dir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not stop fallback sidecar %s: %v\n", FallbackContainerName(name), err)
	}
	return os.RemoveAll(dir)
}

// renderFallbackNginx produces the nginx configuration for the sidecar.
// On a 5xx from the primary upstream — including a connection refused, which
// nginx reports as 502 — nginx re-proxies to the fallback URL, rewriting the
// Host header to the fallback domain so the remote TLS handshake presents the
// correct SNI.
func renderFallbackNginx(spec FallbackSpec) (string, error) {
	fbURL, err := url.Parse(spec.FallbackURL)
	if err != nil {
		return "", fmt.Errorf("invalid fallback url: %w", err)
	}
	if fbURL.Scheme != "http" && fbURL.Scheme != "https" {
		return "", errors.New("fallback url must be http:// or https://")
	}
	fallbackHost := fbURL.Hostname()
	fallbackPort := fbURL.Port()
	if fallbackPort == "" {
		if fbURL.Scheme == "https" {
			fallbackPort = "443"
		} else {
			fallbackPort = "80"
		}
	}
	timeout := spec.FallbackTimeout
	if timeout == "" {
		timeout = constants.FallbackTimeoutDefault
	}

	// A host-network sidecar must not bind :80 (Traefik owns it) — it listens
	// on its allocated loopback port instead.
	listen := "80"
	if spec.HostNetwork {
		listen = fmt.Sprintf("127.0.0.1:%d", spec.ListenPort)
	}

	// Forwarding headers common to both the primary and fallback upstreams.
	fwdHeaders := []nginx.Directive{
		nginx.Dir("proxy_set_header", "X-Real-IP", "$remote_addr"),
		nginx.Dir("proxy_set_header", "X-Forwarded-For", "$proxy_add_x_forwarded_for"),
		nginx.Dir("proxy_set_header", "X-Forwarded-Proto", "$scheme"),
	}

	primary := []nginx.Directive{ //nolint:prealloc // a literal then appends reads better here than a make() with a hand-counted cap
		nginx.Dir("proxy_pass", fmt.Sprintf("http://%s:%s", spec.PrimaryHost, spec.PrimaryPort)),
		nginx.Dir("proxy_http_version", "1.1"),
		nginx.Dir("proxy_set_header", "Host", "$host"),
	}
	primary = append(primary, fwdHeaders...)
	primary = append(primary,
		nginx.Dir("proxy_set_header", "Upgrade", "$http_upgrade"),
		nginx.Dir("proxy_set_header", "Connection", `"upgrade"`),
		nginx.Dir("proxy_connect_timeout", timeout),
		nginx.Dir("proxy_intercept_errors", "on"),
		nginx.Dir("error_page", "502", "503", "504", "=", "@fallback"),
	)

	// proxy_pass with a variable host forces nginx to resolve at request time
	// via the `resolver` directive (public DNS) instead of resolving the
	// literal hostname once at startup through the host resolver. On a host
	// where srv's dnsmasq maps the fallback domain to 127.0.0.1, a literal
	// proxy_pass would dial loopback and loop back into Traefik forever.
	fallback := []nginx.Directive{ //nolint:prealloc // same: the literal is the documentation
		nginx.Dir("set", "$fb_host", fmt.Sprintf("%q", fallbackHost)),
		nginx.Dir("proxy_pass", fmt.Sprintf("%s://$fb_host:%s$request_uri", fbURL.Scheme, fallbackPort)),
		nginx.Dir("proxy_http_version", "1.1"),
		nginx.Dir("proxy_ssl_server_name", "on"),
		nginx.Dir("proxy_ssl_name", "$fb_host"),
		nginx.Dir("proxy_set_header", "Host", "$fb_host"),
	}
	fallback = append(fallback, fwdHeaders...)

	return nginx.Render(
		nginx.Block("server", nil,
			nginx.Dir("listen", listen),
			nginx.Dir("server_name", "_"),
			nginx.Dir("resolver", "1.1.1.1", "8.8.8.8", "valid=300s", "ipv6=off"),
			nginx.Block("location", []string{"/"}, primary...),
			nginx.Block("location", []string{"@fallback"}, fallback...),
		).WithComment("Generated by srv — fallback proxy sidecar"),
	), nil
}

// docker-compose model for the fallback sidecar. Built from structs and
// marshalled once so the interpolated nginx-conf path and network name are
// YAML-encoded as scalars rather than substituted into the document.
type fbComposeService struct {
	Image         string   `yaml:"image"`
	ContainerName string   `yaml:"container_name"`
	Restart       string   `yaml:"restart"`
	NetworkMode   string   `yaml:"network_mode,omitempty"`
	Volumes       []string `yaml:"volumes,omitempty"`
	Networks      []string `yaml:"networks,omitempty"`
}

type fbComposeNetwork struct {
	Name     string `yaml:"name"`
	External bool   `yaml:"external"`
}

type fbComposeFile struct {
	Services map[string]*fbComposeService `yaml:"services"`
	Networks map[string]fbComposeNetwork  `yaml:"networks,omitempty"`
}

// renderFallbackCompose produces the docker-compose.yml for the sidecar.
//
// A HostNetwork sidecar joins the host network namespace — the only way to
// reach a primary upstream bound to 127.0.0.1 — and is reached by Traefik on
// its loopback listen port. A bridge sidecar joins the srv network, reaches a
// container primary by name, and is reached by Traefik by its own name.
func renderFallbackCompose(spec FallbackSpec, nginxConfDir, networkName string) string {
	svc := &fbComposeService{
		Image:         constants.ImageNginxAlpineSlim,
		ContainerName: FallbackContainerName(spec.Name),
		Restart:       "unless-stopped",
		Volumes:       []string{nginxConfDir + "/nginx.conf:/etc/nginx/conf.d/default.conf:ro"},
	}
	doc := fbComposeFile{Services: map[string]*fbComposeService{"fallback": svc}}

	if spec.HostNetwork {
		svc.NetworkMode = "host"
	} else {
		svc.Networks = []string{"traefik"}
		doc.Networks = map[string]fbComposeNetwork{"traefik": {Name: networkName, External: true}}
	}

	// The marshal error is ignored: the struct is fixed-shape and cannot fail
	// to encode.
	data, _ := yaml.Marshal(&doc)
	return fmt.Sprintf("# Generated by srv — fallback proxy sidecar for %s\n", spec.Name) + string(data)
}
