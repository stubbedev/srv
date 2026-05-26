package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/firewall"
	"github.com/stubbedev/srv/internal/shell"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

// =============================================================================
// doctor command
// =============================================================================

var doctorFlags struct {
	fixPerms bool
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run diagnostic checks",
	Long: `Run diagnostic checks to identify common issues with your srv setup.

Checks performed:
  - Docker availability and status
  - Required ports (80, 443, 8080)
  - Docker network existence
  - Traefik container status
  - Local SSL certificate validity
  - mkcert installation
  - Site metadata validity
  - .env host-loopback references in container-backed sites
  - Ownership of ~/.config/srv (use --fix-perms to repair)`,
	RunE: runDoctor,
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorFlags.fixPerms, "fix-perms", false, "Interactively sudo chown ~/.config/srv back to the current user when files are root-owned")
	doctorCmd.GroupID = GroupSystem
	RootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	ui.Blank()
	ui.Info("Running diagnostics...")
	ui.Blank()

	issues := 0
	issues += checkDocker()
	issues += checkFirewall()
	issues += checkPorts()
	issues += checkNetwork()
	issues += checkTraefik()
	issues += checkDNS()
	issues += checkCertificates()
	issues += checkSitesValid()
	issues += checkSiteEnvHostLoopback()
	issues += checkConfigDirOwnership(doctorFlags.fixPerms)

	// Summary
	ui.Blank()
	if issues == 0 {
		ui.Success("All checks passed!")
	} else {
		ui.Warn("%d issue(s) found", issues)
	}
	ui.Blank()

	return nil
}

// checkDocker verifies Docker is running
func checkDocker() int {
	ui.Bold("Docker")
	if err := docker.EnsureRunning(); err != nil {
		ui.IndentedError(1, "Docker is not running or not installed")
		ui.Blank()
		return 1
	}
	ui.IndentedSuccess(1, "Docker is running")
	ui.Blank()
	return 0
}

// checkFirewall checks firewall status and port accessibility
func checkFirewall() int {
	issues := 0
	ui.Bold("Firewall")
	fwStatus := firewall.CheckPorts()

	if fwStatus.Firewall == firewall.FirewallNone {
		ui.IndentedDim(1, "No active firewall detected")
	} else {
		ui.IndentedDim(1, "Firewall: %s", fwStatus.Firewall)
		if fwStatus.HTTPOpen {
			ui.IndentedSuccess(1, "Port 80 (HTTP) - open")
		} else {
			ui.IndentedWarn(1, "Port 80 (HTTP) - blocked")
			issues++
		}
		if fwStatus.HTTPSOpen {
			ui.IndentedSuccess(1, "Port 443 (HTTPS) - open")
		} else {
			ui.IndentedWarn(1, "Port 443 (HTTPS) - blocked")
			issues++
		}
		if !fwStatus.HTTPOpen || !fwStatus.HTTPSOpen {
			ui.IndentedDim(1, "Run 'srv install' to configure firewall")
		}
	}

	ui.Blank()
	return issues
}

// checkPorts verifies required ports are available or in use by srv
func checkPorts() int {
	issues := 0
	ui.Bold("Ports")

	type portInfo struct {
		port      int
		name      string
		ownedByFn func() bool
		container string
	}
	ports := []portInfo{
		{constants.PortHTTP, constants.PortNameHTTP, traefik.IsRunning, docker.ContainerTraefik},
		{constants.PortHTTPS, constants.PortNameHTTPS, traefik.IsRunning, docker.ContainerTraefik},
		{constants.PortInternal, constants.PortNameInternal, traefik.IsRunning, docker.ContainerTraefik},
		{constants.PortDashboard, constants.PortNameDashboard, traefik.IsRunning, docker.ContainerTraefik},
		{constants.PortDNS, constants.PortNameDNS, traefik.IsDNSRunning, docker.ContainerDNS},
	}

	for _, p := range ports {
		if traefik.CheckPortAvailable(p.port) {
			ui.IndentedDim(1, ":%d (%s) - available", p.port, p.name)
			continue
		}

		if p.ownedByFn() {
			version := docker.GetContainerImageVersion(p.container)
			ui.IndentedSuccess(1, ":%d (%s) - in use by srv [%s:%s]", p.port, p.name, p.container, version)
			continue
		}

		// Port is occupied by a foreign process — identify it.
		conflict := traefik.PortConflict{Port: p.port, Name: p.name, Process: shell.IdentifyPortProcess(fmt.Sprintf("%d", p.port))}
		if conflict.Process != "" {
			ui.IndentedWarn(1, ":%d (%s) - in use by %s", p.port, p.name, conflict.Process)
		} else {
			ui.IndentedWarn(1, ":%d (%s) - in use by an unknown process", p.port, p.name)
		}
		ui.IndentedDim(2, "stop it with: %s", conflict.StopHint())
		issues++
	}

	ui.Blank()
	return issues
}

// checkNetwork verifies Docker network exists
func checkNetwork() int {
	ui.Bold("Docker Network")
	cfg, err := config.Load()
	if err != nil {
		ui.IndentedError(1, "Failed to load config: %v", err)
		ui.Blank()
		return 1
	}

	if docker.NetworkExists(cfg.NetworkName) {
		ui.IndentedSuccess(1, "Network '%s' exists", cfg.NetworkName)
	} else {
		ui.IndentedWarn(1, "Network '%s' does not exist", cfg.NetworkName)
		ui.IndentedDim(1, "Run 'srv install' to create it")
		ui.Blank()
		return 1
	}

	ui.Blank()
	return 0
}

// checkTraefik verifies Traefik container is running
func checkTraefik() int {
	ui.Bold("Traefik")
	if traefik.IsRunning() {
		ui.IndentedSuccess(1, "Container is running")
		ui.Blank()
		return 0
	}

	ui.IndentedWarn(1, "Container is not running")
	ui.IndentedDim(1, "Run 'srv install' to start")
	ui.Blank()
	return 1
}

// checkDNS verifies DNS server status and configuration
func checkDNS() int {
	issues := 0
	ui.Bold("DNS Server")

	// Check if there are any local domains registered
	localDomains, _ := traefik.LoadLocalDomains()
	hasLocalDomains := len(localDomains) > 0

	if traefik.IsDNSRunning() {
		ui.IndentedSuccess(1, "Container is running")

		// Only check DNS resolution if there are local domains to test against
		if hasLocalDomains {
			testDomain := localDomains[0]
			if traefik.CheckDNS(testDomain) {
				ui.IndentedSuccess(1, "Responding to queries")
			} else {
				ui.IndentedWarn(1, "Not responding to queries")
				issues++
			}

			if traefik.CheckSystemDNS(testDomain) {
				ui.IndentedSuccess(1, "System DNS configured")
			} else {
				ui.IndentedWarn(1, "System DNS not configured")
				ui.IndentedDim(1, "Try removing and re-adding a local site to trigger DNS setup")
				issues++
			}
		} else {
			ui.IndentedDim(1, "No local domains registered")
		}
	} else {
		// DNS container not running is only an issue if there are local domains
		if hasLocalDomains {
			ui.IndentedWarn(1, "Container is not running")
			ui.IndentedDim(1, "Run 'srv install' to start")
			issues++
		} else {
			ui.IndentedDim(1, "Not running (no local domains registered)")
		}
	}

	ui.Blank()
	return issues
}

// checkCertificates verifies mkcert installation and certificate status
func checkCertificates() int {
	issues := 0
	ui.Bold("Local SSL Certificates")

	if err := traefik.CheckMkcert(); err != nil {
		ui.IndentedWarn(1, "mkcert is not installed")
		ui.IndentedDim(1, "Install mkcert for local HTTPS support")
		ui.Blank()
		return 1
	}

	ui.IndentedSuccess(1, "mkcert is installed")

	if traefik.IsCAInstalled() {
		ui.IndentedSuccess(1, "CA is installed in system trust store")
	} else {
		ui.IndentedWarn(1, "CA not installed")
		ui.IndentedDim(1, "CA will be auto-installed on first 'srv add --local'")
		issues++
	}

	issues += checkCertificateExpiry()

	ui.Blank()
	return issues
}

// checkCertificateExpiry checks for expired or expiring certificates
func checkCertificateExpiry() int {
	certs := traefik.ListLocalCerts()
	if len(certs) == 0 {
		ui.IndentedDim(1, "No local certificates (generated when adding local sites)")
		return 0
	}

	expired := 0
	expiringSoon := 0
	for _, cert := range certs {
		if cert.IsExpired {
			expired++
		} else if cert.DaysLeft <= constants.CertExpiryWarningDays {
			expiringSoon++
		}
	}

	if expired > 0 {
		ui.IndentedError(1, "%d certificate(s) EXPIRED", expired)
		ui.IndentedDim(1, "Certificates auto-renew on 'srv start'")
		return 1
	}

	if expiringSoon > 0 {
		ui.IndentedWarn(1, "%d certificate(s) expiring soon", expiringSoon)
		return 1
	}

	ui.IndentedSuccess(1, "%d certificate(s) valid", len(certs))
	return 0
}

// checkSitesValid validates every site's metadata.yml so users learn about
// hand-edits that won't be hot-reloaded before they hit an error at runtime.
func checkSitesValid() int {
	ui.Bold("Site Metadata")
	sites, err := site.List()
	if err != nil {
		ui.IndentedWarn(1, "Could not list sites: %v", err)
		ui.Blank()
		return 1
	}
	if len(sites) == 0 {
		ui.IndentedDim(1, "No sites registered")
		ui.Blank()
		return 0
	}
	issues := 0
	for _, s := range sites {
		meta, err := site.ReadSiteMetadata(s.Name)
		if err != nil {
			ui.IndentedWarn(1, "%s: %v", s.Name, err)
			issues++
			continue
		}
		if err := site.ValidateMetadata(meta); err != nil {
			ui.IndentedWarn(1, "%s: %v", s.Name, err)
			issues++
			continue
		}
	}
	if issues == 0 {
		ui.IndentedSuccess(1, "%d site(s) valid", len(sites))
	}
	ui.Blank()
	return issues
}

// checkSiteEnvHostLoopback scans every container-backed site's `.env` for
// host-loopback references that won't resolve from inside the container.
// Applies to every site whose app code runs in a container with its own
// loopback namespace (compose, dockerfile).
//
// Heuristics:
//   - lines matching `*_HOST(S)?=...127.0.0.1` or `*_ENDPOINT=...://127.0.0.1...`
//   - commented lines (starting with #) are skipped
//   - case-insensitive variable name match
func checkSiteEnvHostLoopback() int {
	ui.Bold(".env host references")
	sites, err := site.List()
	if err != nil {
		ui.IndentedWarn(1, "Could not list sites: %v", err)
		ui.Blank()
		return 1
	}

	totalHits := 0
	checked := 0
	for _, s := range sites {
		if s.Type == site.SiteTypeStatic {
			continue
		}
		hits := scanEnvForHostLoopback(filepath.Join(s.Dir, ".env"))
		if len(hits) == 0 {
			continue
		}
		checked++
		ui.IndentedWarn(1, "%s: %d .env entr%s point at 127.0.0.1", s.Name, len(hits), plural(len(hits), "y", "ies"))
		for _, h := range hits {
			ui.IndentedDim(2, "%s", h)
		}
		totalHits += len(hits)
	}

	if totalHits == 0 {
		ui.IndentedDim(1, "No host-loopback references found in site .env files")
		ui.Blank()
		return 0
	}

	ui.Blank()
	ui.IndentedDim(1, "These sites run in a container; 127.0.0.1 inside the container is the container itself.")
	ui.IndentedDim(1, "Fix one of:")
	ui.IndentedDim(2, "(a) rewrite to host.docker.internal — works because srv adds extra_hosts")
	ui.IndentedDim(2, "(b) attach the site's container to the host service's docker network and use its container name")
	ui.IndentedDim(2, "    srv network attach <site> <docker-network>")
	ui.Blank()
	return checked
}

func plural(n int, singular, pluralForm string) string {
	if n == 1 {
		return singular
	}
	return pluralForm
}

// envLoopbackPattern matches "<NAME>=<value containing 127.0.0.1>" where NAME
// is a typical host-pointing config key. Tolerates whitespace and quoted
// values. Anchored to start-of-line so commented entries (#…) skip.
var envLoopbackPattern = regexp.MustCompile(`(?i)^\s*([A-Z][A-Z0-9_]*?)(_HOST|_HOSTS|_ENDPOINT|_URL|_DSN|_URI)\s*=\s*[\"']?[^\"'\n]*127\.0\.0\.1`)

func scanEnvForHostLoopback(path string) []string {
	f, err := os.Open(path) //nolint:gosec // path comes from site metadata (trusted)
	if err != nil {
		return nil
	}
	defer f.Close() //nolint:errcheck

	var hits []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if envLoopbackPattern.MatchString(line) {
			hits = append(hits, trimmed)
		}
	}
	return hits
}

// checkConfigDirOwnership walks ~/.config/srv looking for root-owned files
// when the current user is not root. Such files break every subsequent write
// (site metadata, generated Dockerfile, compose YAML). With --fix-perms it
// prompts the user once and runs `sudo chown -R <user>:<group> <root>`.
//
// Skipped on non-Linux/Darwin platforms where os.Stat doesn't yield Unix
// uid/gid info.
func checkConfigDirOwnership(fix bool) int {
	ui.Bold("Config dir ownership")

	if currentUID() == 0 {
		ui.IndentedDim(1, "Running as root — ownership checks skipped")
		ui.Blank()
		return 0
	}

	cfg, err := config.Load()
	if err != nil {
		ui.IndentedWarn(1, "Could not load config: %v", err)
		ui.Blank()
		return 1
	}

	wrong, err := findRootOwnedPaths(cfg.Root)
	if err != nil {
		ui.IndentedWarn(1, "Could not walk config dir: %v", err)
		ui.Blank()
		return 1
	}
	if len(wrong) == 0 {
		ui.IndentedSuccess(1, "All files in %s are owned by the current user", cfg.Root)
		ui.Blank()
		return 0
	}

	ui.IndentedWarn(1, "%d path(s) in %s are root-owned", len(wrong), cfg.Root)
	preview := wrong
	if len(preview) > 5 {
		preview = preview[:5]
	}
	for _, p := range preview {
		ui.IndentedDim(2, "%s", p)
	}
	if len(wrong) > len(preview) {
		ui.IndentedDim(2, "… and %d more", len(wrong)-len(preview))
	}

	if !fix {
		ui.IndentedDim(1, "Re-run with --fix-perms to chown them back to the current user")
		ui.Blank()
		return 1
	}

	if err := sudoChownTree(cfg.Root); err != nil {
		ui.IndentedError(1, "chown failed: %v", err)
		ui.Blank()
		return 1
	}
	ui.IndentedSuccess(1, "Repaired ownership of %s", cfg.Root)
	ui.Blank()
	return 0
}

// findRootOwnedPaths walks root and collects paths whose owning uid is 0 (or
// whose uid does not match the current user). Stops after collecting a small
// cap so we don't blow up on misconfigured systems with thousands of files.
func findRootOwnedPaths(root string) ([]string, error) {
	const maxFindings = 256
	currentUser := uint32(currentUID())
	var hits []string
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil
	}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // best-effort; skip unreadable entries
		}
		if len(hits) >= maxFindings {
			return filepath.SkipDir
		}
		if uid, ok := statUID(info); ok && uid != currentUser {
			hits = append(hits, path)
		}
		return nil
	})
	return hits, err
}

// sudoChownTree runs `sudo chown -R <uid>:<gid> <root>` so the entire srv
// config tree returns to the invoking user's ownership. Uses the shared
// shell.Default runner so tests can swap it.
func sudoChownTree(root string) error {
	user := fmt.Sprintf("%d:%d", currentUID(), currentGID())
	return shell.Default.SudoRun("chown", "-R", user, root)
}

// =============================================================================
// update command
// =============================================================================

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update Traefik and DNS images",
	Long: `Pull the latest Traefik and DNS images and restart the containers.

This ensures you're running the latest versions with security
patches and new features.`,
	RunE: runUpdate,
}

func init() {
	updateCmd.GroupID = GroupSystem
	RootCmd.AddCommand(updateCmd)
}

func runUpdate(cmd *cobra.Command, args []string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}

	// Pull both images
	ui.Info("Pulling latest images...")
	if err := docker.Pull(docker.ImageTraefik); err != nil {
		return fmt.Errorf("failed to pull Traefik image: %w", err)
	}
	if err := docker.Pull(docker.ImageDNS); err != nil {
		return fmt.Errorf("failed to pull DNS image: %w", err)
	}

	// Recreate containers if running
	if traefik.IsRunning() || traefik.IsDNSRunning() {
		ui.Info("Recreating containers...")
		if err := traefik.RecreateTraefik(); err != nil {
			return fmt.Errorf("failed to recreate containers: %w", err)
		}
		ui.Success("Traefik and DNS updated and restarted")
	} else {
		ui.Success("Images updated")
		ui.Dim("Run 'srv install' to start containers")
	}

	return nil
}

// =============================================================================
// version command
// =============================================================================

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version info",
	Run: func(cmd *cobra.Command, args []string) {
		ui.Info("srv %s", Version)
		if Commit != constants.DefaultCommit {
			ui.Dim("Commit: %s", Commit)
		}
		if BuildDate != constants.DefaultBuildDate {
			ui.Dim("Built:  %s", BuildDate)
		}
	},
}

func init() {
	RootCmd.AddCommand(versionCmd)
}
