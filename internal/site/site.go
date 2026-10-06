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
// generation, including Traefik label builders that other srv-managed
// site types reuse).
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
	ExtraNetworks      []string // Additional external Docker networks the site joins
	DaemonServed       bool     // Served by the daemon's embedded HTTP server (no container)
	SPA                bool     // Static site options, honoured by both the nginx renderer and the daemon server
	Cache              bool
	CORS               bool
}

// Domain returns the canonical (first) hostname for the site, or "" if none.
func (s *Site) Domain() string {
	if s == nil || len(s.Domains) == 0 {
		return ""
	}
	return s.Domains[0]
}

// routed reports whether a daemon-served site's Traefik route was in place
// when the site was loaded — its equivalent of a running container.
func (s *Site) routed() bool {
	return s.Status == constants.StatusRunning
}

// loadSite loads the site whose config directory is sites/<name>.
// Returns the site and whether it needs a status check.
func loadSite(cfg *config.Config, name string) (Site, bool) {
	meta, err := ReadSiteMetadata(name)
	if err != nil || meta == nil {
		// No valid metadata - skip this directory
		return Site{Name: name, IsBroken: true}, false
	}
	return siteFromMetadata(cfg, name, meta)
}

// siteFromMetadata builds the Site for name from its parsed metadata, taking
// over meta's slices (callers hand in a freshly read or built value they no
// longer mutate). Returns the site and whether it needs a container status
// check.
func siteFromMetadata(cfg *config.Config, name string, meta *SiteMetadata) (Site, bool) {
	s := Site{Name: name}
	s.Domains = meta.Domains
	s.IsLocal = meta.IsLocal
	s.Wildcard = meta.Wildcard
	s.Type = meta.Type
	s.ServiceName = meta.ServiceName
	s.ComposeServiceName = meta.ComposeServiceName
	s.Profile = meta.Profile
	s.Port = meta.Port
	s.Dir = meta.ProjectPath
	s.ExtraNetworks = meta.ExtraNetworks
	s.DaemonServed = meta.DaemonServed
	s.SPA = meta.SPA
	s.Cache = meta.Cache
	s.CORS = meta.CORS

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
	case SiteTypeStatic, SiteTypeDockerfile:
		if meta.DaemonServed {
			// No containers exist, so "running" simply means the Traefik
			// route config is in place; no Docker probe is needed.
			if _, err := os.Stat(traefik.SiteRouteConfigPath(cfg, name)); err == nil {
				s.Status = constants.StatusRunning
			} else {
				s.Status = constants.StatusStopped
			}
			return s, false
		}
		// srv-managed sites have their compose file in the srv config dir
		s.ComposeDir = SiteConfigDir(cfg, name)
	default:
		// Compose sites use the project directory
		s.ComposeDir = meta.ProjectPath
	}

	return s, true // Needs status check
}

// siteContainerStatus returns the container status for a site using the most
// efficient available method:
//   - Static/Dockerfile sites have a single container with a known name → Docker SDK inspect (no subprocess).
//   - Compose sites have their containers associated with a working-dir label → Docker SDK list (no subprocess).
//   - Fallback (no service name, unknown type): subprocess docker compose ps.
func siteContainerStatus(s Site) string {
	switch s.Type {
	case SiteTypeStatic, SiteTypeDockerfile:
		// Single-container sites: service name IS the container name.
		if s.ServiceName != "" {
			if status, err := docker.ContainerStatusByName(s.ServiceName); err == nil {
				return status
			}
			// Transient inspect failure (daemon restarting, socket hiccup):
			// fall through to the subprocess probe below rather than
			// reporting a running site as stopped.
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
//
// It uses a fixed pool of at most MaxStatusWorkers goroutines draining a jobs
// channel, rather than one goroutine per site, so a large site list does not
// spawn an unbounded number of goroutines. Each job writes its result into a
// distinct sites[i] element, so direct writes are race-free without a result
// channel.
func fetchSiteStatuses(sites []Site, indices []int) {
	if len(indices) == 0 {
		return
	}

	workers := min(constants.MaxStatusWorkers, len(indices))

	jobs := make(chan int)
	var wg sync.WaitGroup

	for range workers {
		wg.Go(func() {
			for i := range jobs {
				sites[i].Status = statusForIndex(sites, i)
			}
		})
	}

	for _, idx := range indices {
		jobs <- idx
	}
	close(jobs)
	wg.Wait()
}

// statusForIndex returns the container status for sites[i], recovering from a
// panic in the status probe by reporting "unknown" so one misbehaving site
// cannot take down a pool worker.
func statusForIndex(sites []Site, i int) (status string) {
	defer func() {
		if r := recover(); r != nil {
			status = "unknown"
		}
	}()
	return siteContainerStatus(sites[i])
}

// ListBasic returns all registered sites without probing container status.
// Prefer it over List whenever only names/types/domains are needed: List
// issues one Docker client + API roundtrip per site, which at 40 sites costs
// ~30ms locally (an order of magnitude more against Docker Desktop) — pure
// waste for shell completion and the daemon's mapping refresh.
func ListBasic() ([]Site, error) {
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

	var sites []Site
	for _, entry := range entries {
		// Only process directories (site config dirs).
		// Skip internal directories prefixed with "_" (e.g. _proxy-* cert dirs).
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		site, _ := loadSite(cfg, entry.Name())
		sites = append(sites, site)
	}
	return sites, nil
}

// List returns all registered sites.
// Container status checks are done in parallel for better performance.
func List() ([]Site, error) {
	sites, err := ListBasic()
	if err != nil {
		return nil, err
	}

	// Fetch container status in parallel for the sites that can have one.
	var validSiteIndices []int
	for i, s := range sites {
		if !s.IsBroken {
			validSiteIndices = append(validSiteIndices, i)
		}
	}
	fetchSiteStatuses(sites, validSiteIndices)
	return sites, nil
}

// Get returns a specific site by name.
// It loads all registered sites without status probes. Prefer GetByName when
// you only need a single site.
func Get(name string) (*Site, error) {
	sites, err := ListBasic()
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
	return getByName(name, true)
}

// getByName loads one site: a stat of its config dir plus its metadata read.
// probe adds the container status lookup, a Docker API round trip that
// lifecycle operations skip because they act on the site regardless.
func getByName(name string, probe bool) (*Site, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	fi, err := os.Stat(SiteConfigDir(cfg, name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("site not found: %s", name)
		}
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("site not found: %s", name)
	}

	s, needsStatus := loadSite(cfg, name)
	if probe && needsStatus {
		s.Status = siteContainerStatus(s)
	}
	return &s, nil
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
		return absPath, nil //nolint:nilerr // a path that does not exist yet is not an error here — the absolute path is the answer
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
