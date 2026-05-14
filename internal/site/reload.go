// Package site — reload.go implements the idempotent Reload(name) entry point
// used by the daemon watcher and the `srv reload` CLI. Reload re-applies
// everything derivable from metadata.yml: artifact regeneration, Traefik
// routing config, mkcert SAN coverage, and local DNS registration. It does
// NOT restart user containers — callers decide whether a restart is needed
// (label-based sites need one; compose sites pick up file-provider changes
// without restart).
package site

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/traefik"
)

// ReloadResult describes the work Reload performed for a single site.
type ReloadResult struct {
	Name            string
	Skipped         bool // true when metadata hash matches last-applied; no work was done
	NeedsRestart    bool // true when label-based artifacts changed and a container restart is required
	RegeneratedCert bool
	CertCovered     bool // false when local cert could not be issued (e.g. mkcert missing)
	DNSRegistered   int  // count of domains registered with the local resolver
	Warnings        []string
}

// reloadStateFile is the hidden file inside each site's config dir that
// remembers the hash of the last successfully-applied metadata.yml. Used
// by Reload to short-circuit no-op reapply attempts.
const reloadStateFile = ".reload-state"

func reloadStatePath(cfg *config.Config, name string) string {
	return filepath.Join(SiteConfigDir(cfg, name), reloadStateFile)
}

func computeMetadataHash(meta *SiteMetadata) string {
	// Marshal via yaml to capture every field (including pointers) without
	// pulling in reflect.DeepEqual. The hash is stable as long as the YAML
	// encoder produces deterministic output for our schema, which yaml.v3
	// does for non-map fields (maps are stable when keys are strings).
	data, err := yaml.Marshal(meta)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func readLastReloadHash(cfg *config.Config, name string) string {
	b, err := os.ReadFile(reloadStatePath(cfg, name))
	if err != nil {
		return ""
	}
	return string(b)
}

func writeLastReloadHash(cfg *config.Config, name, hash string) {
	_ = os.WriteFile(reloadStatePath(cfg, name), []byte(hash), constants.FilePermDefault)
}

// Reload reads the site's metadata.yml and re-applies every artifact derivable
// from it. Always idempotent: calling repeatedly with no metadata change is a
// no-op for compose sites and a deterministic regeneration for srv-managed sites.
// Returns an error only when the site cannot be validated or written; cert /
// DNS subsystem failures are reported as Warnings on the result.
func Reload(name string) (*ReloadResult, error) {
	meta, err := ReadSiteMetadata(name)
	if err != nil {
		return nil, fmt.Errorf("read metadata: %w", err)
	}
	if meta == nil {
		return nil, fmt.Errorf("site not found: %s", name)
	}
	if err := ValidateMetadata(meta); err != nil {
		return nil, err
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	res := &ReloadResult{Name: name}

	// Short-circuit when nothing changed since the last apply. Daemon-driven
	// reloads on the same site fire repeatedly during editor saves; this is
	// the cheapest possible no-op for those.
	currentHash := computeMetadataHash(meta)
	if currentHash != "" && currentHash == readLastReloadHash(cfg, name) {
		res.Skipped = true
		return res, nil
	}

	// Regenerate generated artifacts (with force=true) so the on-disk view
	// matches the current metadata. For srv-managed types this also implies
	// a container restart is required to pick up new Traefik labels.
	switch meta.Type {
	case SiteTypePHP:
		// Re-render nginx.conf + docker-compose.yml. The Dockerfile is also
		// regenerated; if a rebuild is needed, the user must run `srv restart`.
		if err := WritePHPSiteConfig(name, *meta, PHPSiteInfoFromMetadata(*meta), true); err != nil {
			return res, fmt.Errorf("regenerate PHP config: %w", err)
		}
		res.NeedsRestart = true
	case SiteTypeStatic:
		if err := WriteStaticSiteConfig(name, *meta, true); err != nil {
			return res, fmt.Errorf("regenerate static config: %w", err)
		}
		res.NeedsRestart = true
	case SiteTypeNode, SiteTypeRuby, SiteTypePython, SiteTypeDockerfile:
		// These have their own Write helpers; regenerating their compose
		// file picks up label changes. Caller restarts the container.
		// (Skipping explicit per-type re-write here keeps Reload type-agnostic;
		// a future P-phase introduces a unified WriteSiteConfig dispatcher.)
		res.NeedsRestart = true
	case SiteTypeCompose:
		// Compose sites use the Traefik file provider. Refresh that file in place;
		// no container restart needed for routing changes.
		if err := traefik.WriteSiteRouteConfig(cfg, traefik.SiteRouteConfig{
			Name:        name,
			Domains:     meta.Domains,
			ServiceName: meta.ServiceName,
			Port:        meta.Port,
			IsLocal:     meta.IsLocal,
			Wildcard:    meta.Wildcard,
			Listeners:   meta.Listeners,
		}); err != nil {
			return res, fmt.Errorf("refresh traefik routing: %w", err)
		}
	}

	// Always refresh the per-site extra-routes Traefik file (or remove it
	// when meta has no routes). Picked up by Traefik's file provider with
	// no container restart.
	if err := traefik.WriteRoutesConfig(cfg, buildRouteSet(name, meta)); err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("routes: %v", err))
	}

	// Local SSL + DNS: idempotent; re-issues the cert only if the SAN set
	// would change (handled inside EnsureLocalCert).
	if meta.IsLocal && len(meta.Domains) > 0 {
		for _, d := range meta.Domains {
			if err := traefik.RegisterLocalDomain(d, meta.Wildcard); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("DNS register %s: %v", d, err))
				continue
			}
			res.DNSRegistered++
		}
		if err := traefik.CheckMkcert(); err == nil {
			renewed, certErr := traefik.EnsureLocalCert(name, meta.Domains, meta.Wildcard)
			if certErr != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("cert: %v", certErr))
			} else {
				res.RegeneratedCert = renewed
				res.CertCovered = true
			}
		} else {
			res.Warnings = append(res.Warnings, "mkcert unavailable; local TLS not refreshed")
		}
		if err := traefik.UpdateDynamicConfig(); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("dynamic config: %v", err))
		}
	}

	// Persist the hash so the next Reload can short-circuit. Failures here
	// only cost us an extra regen next time — never block the caller.
	if currentHash != "" {
		writeLastReloadHash(cfg, name, currentHash)
	}
	return res, nil
}

// ValidateMetadata performs basic structural validation on a site's
// metadata.yml. Used by both Reload and the `srv validate` CLI.
//
// Lenient parsing is preserved (unknown keys are ignored at yaml.Unmarshal
// time); validation here covers semantic constraints the YAML structure
// itself cannot express.
func ValidateMetadata(meta *SiteMetadata) error {
	if meta == nil {
		return fmt.Errorf("metadata is nil")
	}
	if len(meta.Domains) == 0 {
		return fmt.Errorf("`domains` must list at least one hostname")
	}
	seen := make(map[string]bool, len(meta.Domains))
	for _, d := range meta.Domains {
		if d == "" {
			return fmt.Errorf("`domains` contains an empty entry")
		}
		if seen[d] {
			return fmt.Errorf("duplicate domain %q", d)
		}
		seen[d] = true
	}
	for _, l := range meta.Listeners {
		if l != constants.ListenerInternal {
			return fmt.Errorf("unknown listener %q (supported: %q)", l, constants.ListenerInternal)
		}
	}
	for i, r := range meta.Routes {
		if r.ID == "" {
			return fmt.Errorf("route #%d has no id", i+1)
		}
		if !routeIDPattern.MatchString(r.ID) {
			return fmt.Errorf("route %q: id must match [a-z0-9-]+", r.ID)
		}
		if (r.Path == "") == (r.PathRegex == "") {
			return fmt.Errorf("route %q: exactly one of `path` or `path_regex` is required", r.ID)
		}
		if r.Rewrite != "" && r.PathRegex == "" {
			return fmt.Errorf("route %q: `rewrite` requires `path_regex`", r.ID)
		}
		if r.PathRegex != "" {
			if _, rerr := regexp.Compile(r.PathRegex); rerr != nil {
				return fmt.Errorf("route %q: invalid path_regex: %w", r.ID, rerr)
			}
		}
		switch r.Upstream.Kind {
		case "":
			return fmt.Errorf("route %q: upstream.kind is required", r.ID)
		case "localhost", "container", "url":
			// valid
		default:
			return fmt.Errorf("route %q: upstream.kind must be one of localhost|container|url, got %q", r.ID, r.Upstream.Kind)
		}
	}
	if meta.Fallback != nil && meta.Fallback.URL == "" {
		return fmt.Errorf("fallback.url is required when fallback is set")
	}
	return nil
}

var routeIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// buildRouteSet compiles metadata.Routes into the Traefik-facing RouteSpec
// list. Validation already happened in ValidateMetadata so errors here are
// programmer bugs and surface via WriteRoutesConfig.
func buildRouteSet(siteName string, meta *SiteMetadata) traefik.SiteRouteSet {
	set := traefik.SiteRouteSet{
		SiteName: siteName,
		Domains:  meta.Domains,
		Wildcard: meta.Wildcard,
		IsLocal:  meta.IsLocal,
	}
	for _, r := range meta.Routes {
		preserve := true
		if r.PreserveHost != nil {
			preserve = *r.PreserveHost
		}
		upstreamURL, err := traefik.ResolveUpstreamURL(r.Upstream.Kind, r.Upstream.Container, r.Upstream.URL, r.Upstream.Port)
		if err != nil {
			// Skip malformed entries; ValidateMetadata covers the common cases
			// and structural errors surface there. Silent skip avoids tearing
			// down good routes when one entry is bad.
			continue
		}
		set.Routes = append(set.Routes, traefik.RouteSpec{
			ID:           r.ID,
			Path:         r.Path,
			PathRegex:    r.PathRegex,
			Rewrite:      r.Rewrite,
			UpstreamURL:  upstreamURL,
			PreserveHost: preserve,
			Priority:     r.Priority,
		})
	}
	return set
}
