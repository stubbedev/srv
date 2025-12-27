// Package traefik handles Traefik configuration generation and management.
package traefik

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/shell"
)

// TraefikYML is the static Traefik configuration template.
const TraefikYML = `api:
  dashboard: true
  insecure: true
  disableDashboardAd: true

log:
  level: INFO

accessLog:
  filePath: /etc/traefik/logs/access.log
  bufferingSize: 100
  filters:
    statusCodes:
      - "200-299"
      - "400-599"

metrics:
  prometheus:
    addEntryPointsLabels: true
    addServicesLabels: true
    addRoutersLabels: true
    buckets:
      - 0.1
      - 0.3
      - 1.2
      - 5.0

tracing:
  serviceName: traefik

entryPoints:
  web:
    address: ":80"
    http:
      redirections:
        entryPoint:
          to: websecure
          scheme: https
  websecure:
    address: ":443"

providers:
  docker:
    exposedByDefault: false
    network: "{{NETWORK}}"
  file:
    directory: /etc/traefik/conf
    watch: true

certificatesResolvers:
  letsencrypt:
    acme:
      email: "{{EMAIL}}"
      storage: /etc/traefik/certs/acme.json
      httpChallenge:
        entryPoint: web
`

// TraefikDynamicYML is the dynamic Traefik configuration template.
// This is the base template; domain-specific certs are added dynamically.
const TraefikDynamicYML = `# Dynamic Traefik configuration
# Domain-specific TLS certificates are added below by srv
tls:
  certificates: []
`

// dockerComposeTemplate is the Traefik docker-compose template.
// Use DockerComposeTemplate() to get the template with variables substituted.
// Note: DNS_HTTP_USER and DNS_HTTP_PASS are randomly generated per installation
// for the dnsmasq HTTP admin interface (not exposed externally, but secured anyway).
const dockerComposeTemplate = `services:
  traefik:
    image: {{TRAEFIK_IMAGE}}
    container_name: {{TRAEFIK_CONTAINER}}
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
      - "8080:8080"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./conf/traefik.yml:/etc/traefik/traefik.yml:ro
      - ./conf:/etc/traefik/conf:ro
      - ./certs:/etc/traefik/certs
      - ./logs:/etc/traefik/logs
    networks:
      - traefik

  dns:
    image: {{DNS_IMAGE}}
    container_name: {{DNS_CONTAINER}}
    restart: unless-stopped
    ports:
      - "127.0.0.1:53:53/udp"
    environment:
      - HTTP_USER={{DNS_HTTP_USER}}
      - HTTP_PASS={{DNS_HTTP_PASS}}
    volumes:
      - ./dnsmasq.conf:/etc/dnsmasq.conf:ro
    logging:
      driver: none

networks:
  traefik:
    name: {{NETWORK}}
    external: true
`

// generateRandomString generates a random hex string of specified length.
func generateRandomString(length int) string {
	bytes := make([]byte, length/2+1)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to a fixed string if crypto/rand fails (shouldn't happen)
		return "srv_" + fmt.Sprintf("%d", os.Getpid())
	}
	return hex.EncodeToString(bytes)[:length]
}

// DockerComposeTemplate returns the docker-compose template with variables substituted.
func DockerComposeTemplate() string {
	r := strings.NewReplacer(
		"{{TRAEFIK_IMAGE}}", docker.ImageTraefik,
		"{{DNS_IMAGE}}", docker.ImageDNS,
		"{{TRAEFIK_CONTAINER}}", docker.ContainerTraefik,
		"{{DNS_CONTAINER}}", docker.ContainerDNS,
		"{{DNS_HTTP_USER}}", generateRandomString(16),
		"{{DNS_HTTP_PASS}}", generateRandomString(32),
	)
	return r.Replace(dockerComposeTemplate)
}

// DnsmasqConf is the initial dnsmasq configuration (no domains).
// Domains are added dynamically via UpdateDnsmasqConfig().
const DnsmasqConf = `# Local domains managed by srv
# Do not edit manually - changes will be overwritten

# No local domains registered

# Forward all other queries to upstream DNS
server=8.8.8.8
server=8.8.4.4

# Don't read /etc/resolv.conf
no-resolv
`

// EnsureConfig ensures all Traefik configuration files exist.
func EnsureConfig(email string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Create directories
	dirs := []string{
		cfg.TraefikDir,
		cfg.TraefikConfDir(),
		filepath.Join(cfg.TraefikDir, "certs"),
		filepath.Join(cfg.TraefikDir, "logs"),
		cfg.LocalCertsDir(),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Write traefik.yml
	traefikYML := strings.ReplaceAll(TraefikYML, "{{NETWORK}}", cfg.NetworkName)
	traefikYML = strings.ReplaceAll(traefikYML, "{{EMAIL}}", email)
	traefikPath := filepath.Join(cfg.TraefikConfDir(), "traefik.yml")
	if err := os.WriteFile(traefikPath, []byte(traefikYML), 0o644); err != nil {
		return fmt.Errorf("failed to write traefik.yml: %w", err)
	}

	// Write traefik-dynamic.yml
	dynamicPath := filepath.Join(cfg.TraefikConfDir(), "traefik-dynamic.yml")
	if err := os.WriteFile(dynamicPath, []byte(TraefikDynamicYML), 0o644); err != nil {
		return fmt.Errorf("failed to write traefik-dynamic.yml: %w", err)
	}

	// Write docker-compose.yml
	composeYML := strings.ReplaceAll(DockerComposeTemplate(), "{{NETWORK}}", cfg.NetworkName)
	composePath := cfg.TraefikComposePath()
	if err := os.WriteFile(composePath, []byte(composeYML), 0o644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	// Write dnsmasq.conf
	dnsmasqPath := filepath.Join(cfg.TraefikDir, "dnsmasq.conf")
	if err := os.WriteFile(dnsmasqPath, []byte(DnsmasqConf), 0o644); err != nil {
		return fmt.Errorf("failed to write dnsmasq.conf: %w", err)
	}

	// Create acme.json with proper permissions
	acmePath := filepath.Join(cfg.TraefikDir, "certs", "acme.json")
	if _, err := os.Stat(acmePath); os.IsNotExist(err) {
		if err := os.WriteFile(acmePath, []byte("{}"), 0o600); err != nil {
			return fmt.Errorf("failed to create acme.json: %w", err)
		}
	}

	return nil
}

// GetEmail returns the stored Let's Encrypt email or prompts for one.
func GetEmail(prompt bool) (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}

	// Check for existing email
	envPath := cfg.EnvTraefikPath()
	if data, err := os.ReadFile(envPath); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if email, found := strings.CutPrefix(line, "ACME_EMAIL="); found {
				return email, nil
			}
		}
	}

	if !prompt {
		return "", fmt.Errorf("no email configured. Run: srv init")
	}

	// Prompt for email
	var email string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Let's Encrypt Email").
				Description("Used for SSL certificate notifications").
				Placeholder("you@example.com").
				Value(&email).
				Validate(func(s string) error {
					if !strings.Contains(s, "@") || !strings.Contains(s, ".") {
						return fmt.Errorf("please enter a valid email address")
					}
					return nil
				}),
		),
	)

	if err := form.Run(); err != nil {
		return "", err
	}

	// Save email
	if err := SaveEmail(email); err != nil {
		return "", err
	}

	return email, nil
}

// SaveEmail saves the Let's Encrypt email to env.traefik.
func SaveEmail(email string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	content := fmt.Sprintf("ACME_EMAIL=%s\n", email)
	return os.WriteFile(cfg.EnvTraefikPath(), []byte(content), 0o644)
}

// IsRunning checks if Traefik container is running.
func IsRunning() bool {
	return docker.IsContainerRunning(docker.ContainerTraefik)
}

// DashboardURL returns the Traefik dashboard URL.
func DashboardURL() string {
	return "http://localhost:8080/dashboard/"
}

// IsDNSRunning checks if the DNS container is running.
func IsDNSRunning() bool {
	return docker.IsContainerRunning(docker.ContainerDNS)
}

// CheckPortAvailable checks if a port is available for binding.
func CheckPortAvailable(port int) bool {
	portStr := fmt.Sprintf("%d", port)
	inUse, err := shell.CheckPort(portStr)
	if err != nil {
		return true // Assume available if we can't check
	}
	return !inUse
}

// PullTraefikImage pulls the latest Traefik image.
func PullTraefikImage() error {
	return docker.Pull(docker.ImageTraefik)
}

// RestartTraefik restarts the Traefik container.
func RestartTraefik() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	return docker.Compose(cfg.TraefikDir, "restart")
}

// RecreateTraefik recreates Traefik containers with the latest image.
func RecreateTraefik() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	return docker.Compose(cfg.TraefikDir, "up", "-d", "--force-recreate")
}

// Reset removes the entire srv configuration directory for a fresh start.
func Reset() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Stop Traefik containers first if running (ignore errors - best effort cleanup)
	if IsRunning() || IsDNSRunning() {
		// Intentionally ignoring error: we're resetting anyway, compose down may fail
		// if containers are in an inconsistent state
		_ = docker.Compose(cfg.TraefikDir, "down") //nolint:errcheck
	}

	// Remove the config directory
	if err := os.RemoveAll(cfg.Root); err != nil {
		return fmt.Errorf("failed to remove config directory: %w", err)
	}

	// Reset the config cache so it gets reloaded
	config.ResetCache()

	return nil
}
