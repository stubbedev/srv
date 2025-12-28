// Package traefik handles Traefik configuration generation and management.
package traefik

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/hashicorp/go-envparse"
	"gopkg.in/yaml.v3"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
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
      - {{SITES_DIR}}:/etc/traefik/sites:ro
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
// Uses crypto/rand for secure randomness. If that fails (extremely rare),
// falls back to time-based entropy combined with process ID.
func generateRandomString(length int) string {
	bytes := make([]byte, length/2+1)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback: combine current time nanoseconds with PID for entropy
		// This is less secure but still unpredictable enough for internal use
		fallbackBytes := make([]byte, length/2+1)
		now := uint64(time.Now().UnixNano())
		pid := uint64(os.Getpid())
		combined := now ^ (pid << 32) ^ (pid >> 32)
		for i := range fallbackBytes {
			fallbackBytes[i] = byte(combined >> (i * 8))
			combined = combined*6364136223846793005 + 1 // LCG step
		}
		return hex.EncodeToString(fallbackBytes)[:length]
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
		"{{DNS_HTTP_USER}}", generateRandomString(constants.DNSUserLength),
		"{{DNS_HTTP_PASS}}", generateRandomString(constants.DNSPassLength),
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
// If traefik.yml exists, it merges user customizations with the template.
func EnsureConfig(email string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Create directories
	dirs := []string{
		cfg.TraefikDir,
		cfg.TraefikConfDir(),
		filepath.Join(cfg.TraefikDir, constants.CertsSubdir),
		filepath.Join(cfg.TraefikDir, constants.LogsSubdir),
		cfg.SitesDir,
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, constants.DirPermDefault); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Write or merge traefik.yml
	traefikPath := filepath.Join(cfg.TraefikConfDir(), "traefik.yml")
	if err := writeOrMergeTraefikYML(traefikPath, cfg.NetworkName, email); err != nil {
		return err
	}

	// Write traefik-dynamic.yml
	dynamicPath := filepath.Join(cfg.TraefikConfDir(), "traefik-dynamic.yml")
	if err := os.WriteFile(dynamicPath, []byte(TraefikDynamicYML), constants.FilePermDefault); err != nil {
		return fmt.Errorf("failed to write traefik-dynamic.yml: %w", err)
	}

	// Write docker-compose.yml
	composeYML := strings.ReplaceAll(DockerComposeTemplate(), "{{NETWORK}}", cfg.NetworkName)
	composeYML = strings.ReplaceAll(composeYML, "{{SITES_DIR}}", cfg.SitesDir)
	composePath := cfg.TraefikComposePath()
	if err := os.WriteFile(composePath, []byte(composeYML), constants.FilePermDefault); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	// Write dnsmasq.conf
	dnsmasqPath := filepath.Join(cfg.TraefikDir, constants.DnsmasqConfFile)
	if err := os.WriteFile(dnsmasqPath, []byte(DnsmasqConf), constants.FilePermDefault); err != nil {
		return fmt.Errorf("failed to write dnsmasq.conf: %w", err)
	}

	// Create acme.json with proper permissions
	acmePath := filepath.Join(cfg.TraefikDir, constants.CertsSubdir, constants.ACMEJSONFile)
	if _, err := os.Stat(acmePath); os.IsNotExist(err) {
		if err := os.WriteFile(acmePath, []byte("{}"), constants.FilePermACME); err != nil {
			return fmt.Errorf("failed to create acme.json: %w", err)
		}
	}

	return nil
}

// writeOrMergeTraefikYML writes the traefik.yml file, merging with existing config if present.
// User-customizable sections (api, log, accessLog, metrics, tracing) are preserved.
// Managed sections (providers, certificatesResolvers) are always updated.
// For entryPoints, user-added entries are preserved but web/websecure are ensured.
func writeOrMergeTraefikYML(path, networkName, email string) error {
	// Prepare the template with substitutions
	templateYML := strings.ReplaceAll(TraefikYML, "{{NETWORK}}", networkName)
	templateYML = strings.ReplaceAll(templateYML, "{{EMAIL}}", email)

	// Check if file exists
	existingData, err := os.ReadFile(path)
	if err != nil {
		// File doesn't exist, write fresh
		if os.IsNotExist(err) {
			return os.WriteFile(path, []byte(templateYML), constants.FilePermDefault)
		}
		return fmt.Errorf("failed to read existing traefik.yml: %w", err)
	}

	// Parse existing config
	var existing map[string]any
	if err := yaml.Unmarshal(existingData, &existing); err != nil {
		// If parsing fails, overwrite with fresh config
		return os.WriteFile(path, []byte(templateYML), constants.FilePermDefault)
	}

	// Parse template config
	var template map[string]any
	if err := yaml.Unmarshal([]byte(templateYML), &template); err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	// Merge configs
	merged := mergeTraefikConfigs(existing, template)

	// Marshal back to YAML
	output, err := yaml.Marshal(merged)
	if err != nil {
		return fmt.Errorf("failed to marshal merged config: %w", err)
	}

	return os.WriteFile(path, output, constants.FilePermDefault)
}

// mergeTraefikConfigs merges existing config with template.
// - User sections (api, log, accessLog, metrics, tracing) are preserved from existing
// - Managed sections (providers, certificatesResolvers) are taken from template
// - entryPoints are merged: user additions preserved, web/websecure ensured from template
func mergeTraefikConfigs(existing, template map[string]any) map[string]any {
	result := make(map[string]any)

	// User-customizable sections - preserve from existing, fall back to template
	userSections := []string{"api", "log", "accessLog", "metrics", "tracing"}
	for _, section := range userSections {
		if val, ok := existing[section]; ok {
			result[section] = val
		} else if val, ok := template[section]; ok {
			result[section] = val
		}
	}

	// Managed sections - always use template
	managedSections := []string{"providers", "certificatesResolvers"}
	for _, section := range managedSections {
		if val, ok := template[section]; ok {
			result[section] = val
		}
	}

	// entryPoints - merge: preserve user additions, ensure web/websecure from template
	result["entryPoints"] = mergeEntryPoints(existing, template)

	return result
}

// mergeEntryPoints merges entryPoints configs.
// Preserves user-added entrypoints, ensures web and websecure exist from template.
func mergeEntryPoints(existing, template map[string]any) map[string]any {
	result := make(map[string]any)

	// Start with existing entryPoints (preserves user additions)
	if existingEP, ok := existing["entryPoints"].(map[string]any); ok {
		for k, v := range existingEP {
			result[k] = v
		}
	}

	// Ensure web and websecure from template (these are required by srv)
	if templateEP, ok := template["entryPoints"].(map[string]any); ok {
		if web, ok := templateEP["web"]; ok {
			result["web"] = web
		}
		if websecure, ok := templateEP["websecure"]; ok {
			result["websecure"] = websecure
		}
	}

	return result
}

// GetEmail returns the stored Let's Encrypt email or prompts for one.
func GetEmail(prompt bool) (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}

	// Check for existing email using envparse for proper .env file parsing
	envPath := cfg.EnvTraefikPath()
	if file, err := os.Open(envPath); err == nil {
		defer file.Close()
		envMap, err := envparse.Parse(file)
		if err == nil {
			if email, ok := envMap[constants.EnvACMEEmail]; ok && email != "" {
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

	// Write in standard .env format (KEY=value with newline)
	content := fmt.Sprintf("%s=%s\n", constants.EnvACMEEmail, email)
	return os.WriteFile(cfg.EnvTraefikPath(), []byte(content), constants.FilePermDefault)
}

// IsRunning checks if Traefik container is running.
func IsRunning() bool {
	return docker.IsContainerRunning(docker.ContainerTraefik)
}

// DashboardURL returns the Traefik dashboard URL.
func DashboardURL() string {
	return fmt.Sprintf("%s%s:%d/dashboard/", constants.SchemeHTTPPrefix, constants.LocalhostIP, constants.PortDashboard)
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
