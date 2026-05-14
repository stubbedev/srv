// Package site handles site management operations.
//
// site.go owns the core Site type plus the discovery layer that turns
// ~/.config/srv/sites/<name>/metadata.yml into a Site value, fetches its
// runtime status via Docker, and exposes List/Get for the rest of the
// codebase. Path helpers (ResolvePath, IsLocalDomain, SanitizeName, Exists)
// live here too — they're tiny and tightly coupled to Site discovery.
//
// Related files: compose.go (docker-compose parsing), metadata.go (the
// on-disk metadata.yml schema and I/O), static.go (static-site config
// generation, including Traefik label builders that PHP and other site
// types reuse).
package site

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/traefik"
)

type Site struct {
	Name               string   // Name of the site (directory name in sites/)
	Dir                string   // Resolved directory path (project directory)
	Domains            []string // All hostnames; Domains[0] is canonical
	IsLocal            bool     // Whether it uses local SSL
	Wildcard           bool     // Match apex + one-level subdomains
	Type               SiteType // compose or static
	IsBroken           bool     // Whether the project directory exists
	Status             string   // Container status
	ServiceName        string   // Container name (for Traefik routing)
	ComposeServiceName string   // Docker Compose service name (for compose commands)
	Profile            string   // Docker Compose profile (if service uses profiles)
	Port               int      // Port (for compose sites)
	ComposeDir         string   // Directory containing docker-compose.yml (may differ from Dir for static sites)
}

// Domain returns the canonical (first) hostname for the site, or "" if none.
func (s *Site) Domain() string {
	if s == nil || len(s.Domains) == 0 {
		return ""
	}
	return s.Domains[0]
}

type siteStatusResult struct {
	index  int
	status string
}

// loadSiteFromDir loads site information from a site config directory.
// Returns the site and whether it needs a status check.
func loadSiteFromDir(cfg *config.Config, entry os.DirEntry) (Site, bool) {
	s := Site{
		Name: entry.Name(),
	}

	// Read site metadata
	meta, err := ReadSiteMetadata(entry.Name())
	if err != nil || meta == nil {
		// No valid metadata - skip this directory
		s.IsBroken = true
		return s, false
	}

	s.Domains = append([]string(nil), meta.Domains...)
	s.IsLocal = meta.IsLocal
	s.Wildcard = meta.Wildcard
	s.Type = meta.Type
	s.ServiceName = meta.ServiceName
	s.ComposeServiceName = meta.ComposeServiceName
	s.Profile = meta.Profile
	s.Port = meta.Port
	s.Dir = meta.ProjectPath

	// Fallback: if ComposeServiceName is empty, use ServiceName (backward compatibility)
	if s.ComposeServiceName == "" && s.ServiceName != "" {
		s.ComposeServiceName = s.ServiceName
	}

	// Check if project path exists
	if _, err := os.Stat(meta.ProjectPath); err != nil {
		s.IsBroken = true
		return s, false
	}

	// Determine compose directory based on site type
	switch meta.Type {
	case SiteTypeStatic, SiteTypePHP, SiteTypeNode, SiteTypeRuby, SiteTypePython, SiteTypeDockerfile:
		// srv-managed sites have their compose file in the srv config dir
		s.ComposeDir = SiteConfigDir(cfg, entry.Name())
	default:
		// Compose sites use the project directory
		s.ComposeDir = meta.ProjectPath
	}

	return s, true // Needs status check
}

// siteContainerStatus returns the container status for a site using the most
// efficient available method:
//   - Static/PHP sites have a single container with a known name → Docker SDK inspect (no subprocess).
//   - Compose sites have their containers associated with a working-dir label → Docker SDK list (no subprocess).
//   - Fallback (no service name, unknown type): subprocess docker compose ps.
func siteContainerStatus(s Site) string {
	switch s.Type {
	case SiteTypeStatic, SiteTypePHP, SiteTypeNode, SiteTypeRuby, SiteTypePython, SiteTypeDockerfile:
		// Single-container sites: service name IS the container name.
		if s.ServiceName != "" {
			return docker.ContainerStatusByName(s.ServiceName)
		}
	case SiteTypeCompose:
		// Multi-container compose projects: query by working-dir label.
		if s.ComposeDir != "" {
			return docker.ContainerStatusByComposeDir(s.ComposeDir)
		}
	}
	// Fallback: subprocess docker compose ps (backward compat).
	return docker.ContainerStatus(s.ComposeDir)
}

// fetchSiteStatuses fetches container statuses for sites in parallel.
func fetchSiteStatuses(sites []Site, indices []int) {
	if len(indices) == 0 {
		return
	}

	workers := min(constants.MaxStatusWorkers, len(indices))

	var wg sync.WaitGroup
	statusChan := make(chan siteStatusResult, len(indices))

	// Semaphore for limiting concurrency
	sem := make(chan struct{}, workers)

	for _, idx := range indices {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Recover from any panic so wg.Done() is always called and
			// the collector goroutine is never left blocking forever.
			defer func() {
				if r := recover(); r != nil {
					statusChan <- siteStatusResult{i, "unknown"}
				}
			}()
			sem <- struct{}{}        // Acquire
			defer func() { <-sem }() // Release

			status := siteContainerStatus(sites[i])
			statusChan <- siteStatusResult{i, status}
		}(idx)
	}

	// Wait for all goroutines to complete
	go func() {
		wg.Wait()
		close(statusChan)
	}()

	// Collect results
	for result := range statusChan {
		sites[result.index].Status = result.status
	}
}

// List returns all registered sites.
// Container status checks are done in parallel for better performance.
func List() ([]Site, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(cfg.SitesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Site{}, nil
		}
		return nil, err
	}

	// First pass: collect site metadata (fast, sequential)
	var sites []Site
	var validSiteIndices []int // Indices of sites that need status check

	for _, entry := range entries {
		// Only process directories (site config dirs).
		// Skip internal directories prefixed with "_" (e.g. _proxy-* cert dirs).
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}

		site, needsStatus := loadSiteFromDir(cfg, entry)
		if needsStatus {
			validSiteIndices = append(validSiteIndices, len(sites))
		}
		sites = append(sites, site)
	}

	// Second pass: fetch container status in parallel
	fetchSiteStatuses(sites, validSiteIndices)

	return sites, nil
}

// Get returns a specific site by name.
// It loads all registered sites and therefore performs a parallel status check
// for every site. Prefer GetByName when you only need a single site.
func Get(name string) (*Site, error) {
	sites, err := List()
	if err != nil {
		return nil, err
	}

	for _, s := range sites {
		if s.Name == name {
			return &s, nil
		}
	}

	return nil, fmt.Errorf("site not found: %s", name)
}

// GetByName returns a single site by name without loading all registered sites.
// It reads only that site's metadata and fetches its container status directly,
// making it significantly faster than Get when the full list is not needed.
func GetByName(name string) (*Site, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	// Verify the site config directory exists.
	siteDir := SiteConfigDir(cfg, name)
	if _, err := os.Stat(siteDir); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("site not found: %s", name)
		}
		return nil, err
	}

	// Synthesise a DirEntry-like object using the site name.
	entries, err := os.ReadDir(filepath.Dir(siteDir))
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.Name() == name && entry.IsDir() {
			s, needsStatus := loadSiteFromDir(cfg, entry)
			if needsStatus {
				s.Status = siteContainerStatus(s)
			}
			return &s, nil
		}
	}

	return nil, fmt.Errorf("site not found: %s", name)
}

// ResolvePath resolves a site path to an absolute path.
func ResolvePath(path string) (string, error) {
	// Expand home directory
	if strings.HasPrefix(path, constants.HomeDirPrefix) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[len(constants.HomeDirPrefix):])
	}

	// Make absolute
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	// Resolve symlinks
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		// Path might not exist yet, return absolute path
		return absPath, nil //nolint:nilerr
	}

	return realPath, nil
}

// IsLocalDomain checks if a domain should use local SSL.
func IsLocalDomain(domain string) bool {
	for _, tld := range traefik.LocalDomains {
		if strings.HasSuffix(domain, "."+tld) {
			return true
		}
	}
	return false
}

// SanitizeName creates a valid site name from a path or string.
// Dots are replaced with hyphens so that a path like "myapp.test" becomes
// "myapp-test", which is a valid site name.
func SanitizeName(s string) string {
	// Use base name if path
	s = filepath.Base(s)
	// Replace invalid characters
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, ".", "-")
	s = strings.ToLower(s)
	return s
}

// Exists checks if a site is already registered.
func Exists(name string) bool {
	return HasSiteMetadata(name)
}

// generateStaticContainerName generates a container name for a static site.
// Format: srv_static_<short_hash> where hash is derived from the site name.
func generateStaticContainerName(name string) string {
	hash := sha256.Sum256([]byte(name))
	shortHash := hex.EncodeToString(hash[:])[:constants.StaticContainerHashLength]
	return constants.StaticContainerPrefix + shortHash
}

// StaticSiteOptions holds configuration options for static sites.
