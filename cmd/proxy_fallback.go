// Package cmd — proxy_fallback.go renders the nginx sidecar used by
// `srv proxy add --fallback`. The sidecar fronts the primary upstream and
// transparently re-proxies to a remote URL when the primary returns 5xx.
package cmd

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/ui"
)

// fallbackSpec captures what the sidecar needs to know to generate its config.
type fallbackSpec struct {
	Name            string // proxy name (used in container + dir names)
	PrimaryHost     string // host the sidecar dials for the primary upstream
	PrimaryPort     string
	FallbackURL     string // e.g. https://kontainer.com
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

// fallbackContainerName returns the container name for a fallback sidecar.
func fallbackContainerName(name string) string {
	return "srv-proxy-" + name + "-fallback"
}

// fallbackSiteDir returns the directory for a fallback sidecar's generated
// docker-compose.yml and nginx.conf.
func fallbackSiteDir(cfg *config.Config, name string) string {
	return filepath.Join(cfg.SitesDir, "_proxy-"+name+"-fallback")
}

// findFreeLoopbackPort asks the OS for an unused TCP port on 127.0.0.1 by
// binding port 0 and reading back the assignment. There is an unavoidable
// race between releasing the port and the sidecar binding it, but the window
// is tiny and the sidecar starts immediately after.
func findFreeLoopbackPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
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

// writeFallbackSidecar renders the nginx.conf + docker-compose.yml for a
// fallback sidecar and starts the container. Returns the URL Traefik should
// route to.
func writeFallbackSidecar(cfg *config.Config, spec fallbackSpec) (string, error) {
	if spec.HostNetwork {
		port, err := findFreeLoopbackPort()
		if err != nil {
			return "", err
		}
		spec.ListenPort = port
	}

	dir := fallbackSiteDir(cfg, spec.Name)
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

	if err := docker.ComposeUp(dir); err != nil {
		return "", fmt.Errorf("start fallback sidecar: %w", err)
	}

	// A host-network sidecar is reached on the loopback port it binds; a
	// bridge sidecar is reached by container name on the srv network.
	if spec.HostNetwork {
		return fmt.Sprintf("http://127.0.0.1:%d", spec.ListenPort), nil
	}
	return fmt.Sprintf("http://%s:80", fallbackContainerName(spec.Name)), nil
}

// removeFallbackSidecar stops the sidecar container and deletes its directory.
func removeFallbackSidecar(cfg *config.Config, name string) error {
	dir := fallbackSiteDir(cfg, name)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := docker.ComposeDown(dir); err != nil {
		// Surface the failure so the user knows the container may still be
		// running even after we remove its compose directory. We still proceed
		// with the RemoveAll — leaving the dir on disk after a partial cleanup
		// is worse than a stranded container the user can `docker rm` themselves.
		ui.Warn("could not stop fallback sidecar %s: %v", fallbackContainerName(name), err)
	}
	return os.RemoveAll(dir)
}

// renderFallbackNginx produces the nginx configuration for the sidecar.
// On a 5xx from the primary upstream — including a connection refused, which
// nginx reports as 502 — nginx re-proxies to the fallback URL, rewriting the
// Host header to the fallback domain so the remote TLS handshake presents the
// correct SNI.
func renderFallbackNginx(spec fallbackSpec) (string, error) {
	fbURL, err := url.Parse(spec.FallbackURL)
	if err != nil {
		return "", fmt.Errorf("invalid fallback url: %w", err)
	}
	if fbURL.Scheme != "http" && fbURL.Scheme != "https" {
		return "", fmt.Errorf("fallback url must be http:// or https://")
	}
	fallbackHost := fbURL.Hostname()
	timeout := spec.FallbackTimeout
	if timeout == "" {
		timeout = "2s"
	}

	// A host-network sidecar must not bind :80 (Traefik owns it) — it listens
	// on its allocated loopback port instead.
	listen := "listen 80;"
	if spec.HostNetwork {
		listen = fmt.Sprintf("listen 127.0.0.1:%d;", spec.ListenPort)
	}

	var b strings.Builder
	b.WriteString("# Generated by srv — fallback proxy sidecar\n")
	b.WriteString("server {\n")
	fmt.Fprintf(&b, "    %s\n", listen)
	b.WriteString("    server_name _;\n")
	b.WriteString("    resolver 1.1.1.1 8.8.8.8 valid=300s ipv6=off;\n")
	b.WriteString("\n")
	b.WriteString("    location / {\n")
	fmt.Fprintf(&b, "        proxy_pass http://%s:%s;\n", spec.PrimaryHost, spec.PrimaryPort)
	b.WriteString("        proxy_http_version 1.1;\n")
	b.WriteString("        proxy_set_header Host $host;\n")
	b.WriteString("        proxy_set_header X-Real-IP $remote_addr;\n")
	b.WriteString("        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
	b.WriteString("        proxy_set_header X-Forwarded-Proto $scheme;\n")
	b.WriteString("        proxy_set_header Upgrade $http_upgrade;\n")
	b.WriteString("        proxy_set_header Connection \"upgrade\";\n")
	fmt.Fprintf(&b, "        proxy_connect_timeout %s;\n", timeout)
	b.WriteString("        proxy_intercept_errors on;\n")
	b.WriteString("        error_page 502 503 504 = @fallback;\n")
	b.WriteString("    }\n")
	b.WriteString("\n")
	b.WriteString("    location @fallback {\n")
	fmt.Fprintf(&b, "        set $fb_host \"%s\";\n", fallbackHost)
	fmt.Fprintf(&b, "        proxy_pass %s;\n", spec.FallbackURL)
	b.WriteString("        proxy_http_version 1.1;\n")
	b.WriteString("        proxy_ssl_server_name on;\n")
	b.WriteString("        proxy_ssl_name $fb_host;\n")
	b.WriteString("        proxy_set_header Host $fb_host;\n")
	b.WriteString("        proxy_set_header X-Real-IP $remote_addr;\n")
	b.WriteString("        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
	b.WriteString("        proxy_set_header X-Forwarded-Proto $scheme;\n")
	b.WriteString("    }\n")
	b.WriteString("}\n")
	return b.String(), nil
}

// renderFallbackCompose produces the docker-compose.yml for the sidecar.
//
// A HostNetwork sidecar joins the host network namespace — the only way to
// reach a primary upstream bound to 127.0.0.1 — and is reached by Traefik on
// its loopback listen port. A bridge sidecar joins the srv network, reaches a
// container primary by name, and is reached by Traefik by its own name.
func renderFallbackCompose(spec fallbackSpec, nginxConfDir, networkName string) string {
	if spec.HostNetwork {
		return fmt.Sprintf(`# Generated by srv — fallback proxy sidecar for %s
services:
  fallback:
    image: %s
    container_name: %s
    restart: unless-stopped
    network_mode: host
    volumes:
      - %s/nginx.conf:/etc/nginx/conf.d/default.conf:ro
`, spec.Name, constants.ImageNginxAlpine, fallbackContainerName(spec.Name), nginxConfDir)
	}

	return fmt.Sprintf(`# Generated by srv — fallback proxy sidecar for %s
services:
  fallback:
    image: %s
    container_name: %s
    restart: unless-stopped
    volumes:
      - %s/nginx.conf:/etc/nginx/conf.d/default.conf:ro
    networks:
      - traefik

networks:
  traefik:
    name: %s
    external: true
`, spec.Name, constants.ImageNginxAlpine, fallbackContainerName(spec.Name), nginxConfDir, networkName)
}
