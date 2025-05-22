// Package traefik handles Traefik configuration generation and management.
package traefik

import (
	"bufio"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/huh"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/shell"
)

// LocalDomains are the TLDs used for local development.
var LocalDomains = []string{"test", "local", "localhost"}

// DNS config file paths.
const (
	systemdResolvedConfig = "/etc/systemd/resolved.conf.d/srv-local.conf"
	networkManagerConfig  = "/etc/NetworkManager/dnsmasq.d/srv-local.conf"
	macOSResolverDir      = "/etc/resolver"
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
      - HTTP_USER=admin
      - HTTP_PASS=admin
    volumes:
      - ./dnsmasq.conf:/etc/dnsmasq.conf:ro
    logging:
      driver: none

networks:
  traefik:
    name: {{NETWORK}}
    external: true
`

// DockerComposeTemplate returns the docker-compose template with variables substituted.
func DockerComposeTemplate() string {
	r := strings.NewReplacer(
		"{{TRAEFIK_IMAGE}}", docker.ImageTraefik,
		"{{DNS_IMAGE}}", docker.ImageDNS,
		"{{TRAEFIK_CONTAINER}}", docker.ContainerTraefik,
		"{{DNS_CONTAINER}}", docker.ContainerDNS,
	)
	return r.Replace(dockerComposeTemplate)
}

// DnsmasqConf is the dnsmasq configuration for local domains.
const DnsmasqConf = `# Resolve *.test, *.local, *.localhost to 127.0.0.1
address=/test/127.0.0.1
address=/local/127.0.0.1
address=/localhost/127.0.0.1

# Forward all other queries to upstream DNS
server=8.8.8.8
server=8.8.4.4

# Don't read /etc/resolv.conf
no-resolv

# Log queries (disabled for less noise)
# log-queries
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

// CheckMkcert verifies mkcert is installed and provides installation instructions if not.
func CheckMkcert() error {
	if !shell.Exists("mkcert") {
		if runtime.GOOS == "darwin" {
			return fmt.Errorf("mkcert is not installed.\n  Install with: brew install mkcert")
		}
		return fmt.Errorf("mkcert is not installed.\n  See: https://github.com/FiloSottile/mkcert#installation")
	}
	return nil
}

// IsCAInstalled checks if the mkcert CA is installed.
func IsCAInstalled() bool {
	output, err := shell.MkcertQuiet("-CAROOT")
	if err != nil {
		return false
	}
	caRoot := strings.TrimSpace(string(output))
	if caRoot == "" {
		return false
	}
	_, err = os.Stat(filepath.Join(caRoot, "rootCA.pem"))
	return err == nil
}

// LocalCertsExist checks if local SSL certificates exist for a domain.
func LocalCertsExist(domain string) bool {
	cfg, err := config.Load()
	if err != nil {
		return false
	}
	certFile := filepath.Join(cfg.LocalCertsDir(), domain+".crt")
	keyFile := filepath.Join(cfg.LocalCertsDir(), domain+".key")
	_, certErr := os.Stat(certFile)
	_, keyErr := os.Stat(keyFile)
	return certErr == nil && keyErr == nil
}

// RemoveLocalCerts removes SSL certificates for a specific domain.
func RemoveLocalCerts(domain string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	certFile := filepath.Join(cfg.LocalCertsDir(), domain+".crt")
	keyFile := filepath.Join(cfg.LocalCertsDir(), domain+".key")
	os.Remove(certFile)
	os.Remove(keyFile)
	return nil
}

// GenerateLocalCert generates an SSL certificate for a specific domain using mkcert.
func GenerateLocalCert(domain string) error {
	if err := CheckMkcert(); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	certDir := cfg.LocalCertsDir()
	if err := os.MkdirAll(certDir, 0o755); err != nil {
		return fmt.Errorf("failed to create certs directory: %w", err)
	}

	certFile := filepath.Join(certDir, domain+".crt")
	keyFile := filepath.Join(certDir, domain+".key")

	args := []string{
		"-cert-file", certFile,
		"-key-file", keyFile,
		domain,
	}

	if err := shell.Mkcert(args...); err != nil {
		return fmt.Errorf("failed to generate certificate for %s: %w", domain, err)
	}

	return nil
}

// EnsureLocalCert generates an SSL certificate for a domain if it doesn't exist.
func EnsureLocalCert(domain string) error {
	if LocalCertsExist(domain) {
		return nil
	}
	return GenerateLocalCert(domain)
}

// UpdateDynamicConfig regenerates the Traefik dynamic config with all local domain certs.
func UpdateDynamicConfig() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	certDir := cfg.LocalCertsDir()

	// Find all .crt files in the local certs directory
	var certs []string
	entries, err := os.ReadDir(certDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read certs directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".crt") {
			domain := strings.TrimSuffix(name, ".crt")
			keyFile := filepath.Join(certDir, domain+".key")
			if _, err := os.Stat(keyFile); err == nil {
				certs = append(certs, domain)
			}
		}
	}

	// Build dynamic config
	var content strings.Builder
	content.WriteString("# Dynamic Traefik configuration - generated by srv\n")
	content.WriteString("# Do not edit manually\n")
	content.WriteString("tls:\n")

	if len(certs) == 0 {
		content.WriteString("  certificates: []\n")
	} else {
		content.WriteString("  certificates:\n")
		for _, domain := range certs {
			content.WriteString(fmt.Sprintf("    - certFile: /etc/traefik/certs/local/%s.crt\n", domain))
			content.WriteString(fmt.Sprintf("      keyFile: /etc/traefik/certs/local/%s.key\n", domain))
		}
	}

	dynamicPath := filepath.Join(cfg.TraefikConfDir(), "traefik-dynamic.yml")
	if err := os.WriteFile(dynamicPath, []byte(content.String()), 0o644); err != nil {
		return fmt.Errorf("failed to write dynamic config: %w", err)
	}

	return nil
}

// InstallCA installs the mkcert CA certificate.
func InstallCA() error {
	if !shell.Exists("mkcert") {
		return fmt.Errorf("mkcert is not installed.\n  Install it first: https://github.com/FiloSottile/mkcert#installation")
	}

	if err := shell.Mkcert("-install"); err != nil {
		return fmt.Errorf("failed to install mkcert CA: %w", err)
	}

	return nil
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

// CheckDNS tests if local DNS resolution is working for .test domains.
func CheckDNS() bool {
	output, err := shell.Dig("+short", "@127.0.0.1", "test.test")
	if err != nil {
		return false
	}
	return output == "127.0.0.1"
}

// CheckSystemDNS tests if the system resolves .test domains correctly.
func CheckSystemDNS() bool {
	output, err := shell.Dig("+short", "test.test")
	if err != nil {
		return false
	}
	return output == "127.0.0.1"
}

// DNSResolverType represents the type of DNS resolver on the system.
type DNSResolverType int

const (
	ResolverUnknown DNSResolverType = iota
	ResolverSystemdResolved
	ResolverMacOS
	ResolverNetworkManager
)

// DetectResolver detects the DNS resolver type on the system.
func DetectResolver() DNSResolverType {
	// Check for systemd-resolved
	if _, err := os.Stat("/run/systemd/resolve/stub-resolv.conf"); err == nil {
		return ResolverSystemdResolved
	}

	// Check for macOS resolver directory capability
	if runtime.GOOS == "darwin" {
		return ResolverMacOS
	}

	// Check for NetworkManager
	if shell.Exists("nmcli") {
		return ResolverNetworkManager
	}

	return ResolverUnknown
}

// SetupDNS configures the system to use the local DNS server for .test domains.
// Returns an error if setup fails or requires manual intervention.
func SetupDNS() error {
	resolver := DetectResolver()

	switch resolver {
	case ResolverSystemdResolved:
		return setupSystemdResolved()
	case ResolverMacOS:
		return setupMacOSResolver()
	case ResolverNetworkManager:
		return setupNetworkManager()
	default:
		return fmt.Errorf("unsupported DNS configuration. Please manually configure your system to use 127.0.0.1 for .test, .local, and .localhost domains")
	}
}

// setupSystemdResolved configures systemd-resolved for local domains.
func setupSystemdResolved() error {
	configFile := systemdResolvedConfig
	configDir := filepath.Dir(configFile)

	// Check if already configured
	if _, err := os.Stat(configFile); err == nil {
		return nil // Already configured
	}

	content := `[Resolve]
DNS=127.0.0.1
Domains=~test ~local ~localhost
`

	if err := shell.SudoMkdir(configDir); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := shell.SudoWrite(configFile, content); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	if err := shell.SudoSystemctl("restart", "systemd-resolved"); err != nil {
		return fmt.Errorf("failed to restart systemd-resolved: %w", err)
	}

	return nil
}

// setupMacOSResolver configures macOS resolver for local domains.
func setupMacOSResolver() error {
	if err := shell.SudoMkdir(macOSResolverDir); err != nil {
		return fmt.Errorf("failed to create resolver directory: %w", err)
	}

	for _, domain := range LocalDomains {
		resolverFile := filepath.Join(macOSResolverDir, domain)

		// Check if already configured
		if data, err := os.ReadFile(resolverFile); err == nil {
			if strings.Contains(string(data), "127.0.0.1") {
				continue // Already configured
			}
		}

		if err := shell.SudoWrite(resolverFile, "nameserver 127.0.0.1\n"); err != nil {
			return fmt.Errorf("failed to write resolver file for .%s: %w", domain, err)
		}
	}

	return nil
}

// setupNetworkManager configures NetworkManager to use local DNS for .test domains.
func setupNetworkManager() error {
	configFile := networkManagerConfig
	configDir := filepath.Dir(configFile)

	// Check if already configured
	if _, err := os.Stat(configFile); err == nil {
		return nil // Already configured
	}

	content := `# srv local DNS configuration
server=/test/127.0.0.1
server=/local/127.0.0.1
server=/localhost/127.0.0.1
`

	if err := shell.SudoMkdir(configDir); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := shell.SudoWrite(configFile, content); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	if err := shell.SudoSystemctl("restart", "NetworkManager"); err != nil {
		return fmt.Errorf("failed to restart NetworkManager: %w", err)
	}

	return nil
}

// RemoveDNS removes the DNS configuration set up by SetupDNS.
func RemoveDNS() error {
	resolver := DetectResolver()

	switch resolver {
	case ResolverSystemdResolved:
		if _, err := os.Stat(systemdResolvedConfig); os.IsNotExist(err) {
			return nil // Already removed
		}
		if err := shell.SudoRemove(systemdResolvedConfig); err != nil {
			return fmt.Errorf("failed to remove config file: %w", err)
		}
		return shell.SudoSystemctl("restart", "systemd-resolved")

	case ResolverMacOS:
		for _, domain := range LocalDomains {
			resolverFile := filepath.Join(macOSResolverDir, domain)
			_ = shell.SudoRemove(resolverFile) // Ignore errors for non-existent files
		}
		return nil

	case ResolverNetworkManager:
		if _, err := os.Stat(networkManagerConfig); os.IsNotExist(err) {
			return nil // Already removed
		}
		if err := shell.SudoRemove(networkManagerConfig); err != nil {
			return fmt.Errorf("failed to remove config file: %w", err)
		}
		return shell.SudoSystemctl("restart", "NetworkManager")

	default:
		return nil
	}
}

// GetResolverName returns a human-readable name for the resolver type.
func GetResolverName() string {
	switch DetectResolver() {
	case ResolverSystemdResolved:
		return "systemd-resolved"
	case ResolverMacOS:
		return "macOS resolver"
	case ResolverNetworkManager:
		return "NetworkManager"
	default:
		return "unknown"
	}
}

// CertInfo holds certificate expiry information.
type CertInfo struct {
	Domain    string
	Exists    bool
	ExpiresAt time.Time
	DaysLeft  int
	IsExpired bool
}

// GetLocalCertInfo returns information about a specific local SSL certificate.
func GetLocalCertInfo(domain string) CertInfo {
	cfg, err := config.Load()
	if err != nil {
		return CertInfo{Domain: domain}
	}

	certFile := filepath.Join(cfg.LocalCertsDir(), domain+".crt")
	info := parseCertFile(certFile)
	info.Domain = domain
	return info
}

// ListLocalCerts returns information about all local SSL certificates.
func ListLocalCerts() []CertInfo {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}

	certDir := cfg.LocalCertsDir()
	entries, err := os.ReadDir(certDir)
	if err != nil {
		return nil
	}

	var certs []CertInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".crt") {
			domain := strings.TrimSuffix(name, ".crt")
			certs = append(certs, GetLocalCertInfo(domain))
		}
	}
	return certs
}

// parseCertFile reads a PEM certificate file and returns expiry info.
func parseCertFile(certPath string) CertInfo {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return CertInfo{Exists: false}
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return CertInfo{Exists: true}
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return CertInfo{Exists: true}
	}

	now := time.Now()
	daysLeft := int(cert.NotAfter.Sub(now).Hours() / 24)

	return CertInfo{
		Exists:    true,
		ExpiresAt: cert.NotAfter,
		DaysLeft:  daysLeft,
		IsExpired: now.After(cert.NotAfter),
	}
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

	// Stop Traefik containers first if running
	if IsRunning() || IsDNSRunning() {
		_ = docker.Compose(cfg.TraefikDir, "down")
	}

	// Remove the config directory
	if err := os.RemoveAll(cfg.Root); err != nil {
		return fmt.Errorf("failed to remove config directory: %w", err)
	}

	// Reset the config cache so it gets reloaded
	config.ResetCache()

	return nil
}
