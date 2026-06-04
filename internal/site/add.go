// Package site — add.go is the headless `srv add` pipeline: detect the project
// type, assemble metadata, write the per-site artifacts, issue local certs +
// DNS, and optionally start the containers. It is shared by the CLI (cmd/site_add*)
// and the MCP add_site tool. All decisions arrive as explicit AddOptions fields
// — there is no interactive prompting here — so both surfaces behave identically.
package site

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/validate"
)

// AddOptions is the full, non-interactive description of a site to add.
type AddOptions struct {
	Path         string   // project path (resolved against cwd / parked roots)
	TypeOverride string   // "", "compose", "dockerfile", or "static"
	Name         string   // site name; derived from Domain when empty
	Domain       string   // canonical hostname (required)
	Aliases      []string // extra hostnames
	Port         int      // container port; 0 → DefaultContainerPort
	Local        bool     // local mkcert TLS (otherwise Let's Encrypt)
	Wildcard     bool     // match one-level subdomains (local only)
	InternalHTTP bool     // also expose on the internal plain-HTTP entrypoint
	Service      string   // compose service selector (compose sites)
	Profile      string   // compose profile selector
	SPA          bool     // static-site options
	Cache        bool
	CORS         bool
	Volumes      []VolumeMount // extra bind-mounts
	Force        bool          // overwrite an existing site
	Start        bool          // bring containers up after adding
}

// AddResult reports what Add produced.
type AddResult struct {
	Name     string   `json:"name"`
	Domain   string   `json:"domain"`
	Type     string   `json:"type"`
	IsLocal  bool     `json:"is_local"`
	Warnings []string `json:"warnings,omitempty"`
}

// addSetup is the resolved, validated configuration produced from AddOptions.
type addSetup struct {
	opts               AddOptions
	sitePath           string
	composePath        string
	serviceName        string
	composeServiceName string
	profile            string
	siteName           string
	domain             string
	aliases            []string
	listeners          []string
	port               int
	isStatic           bool
	isDockerfile       bool
	dockerfileInfo     *DockerfileSiteInfo
}

func (s *addSetup) allDomains() []string {
	out := make([]string, 0, 1+len(s.aliases))
	if s.domain != "" {
		out = append(out, s.domain)
	}
	return append(out, s.aliases...)
}

func (s *addSetup) typeLabel() string {
	switch {
	case s.isDockerfile:
		return "dockerfile"
	case s.isStatic:
		return "static"
	default:
		return "compose"
	}
}

// Add runs the full add pipeline. It returns an error for any fatal step
// (bad input, file write failure); cert/DNS/start failures are non-fatal and
// returned as AddResult.Warnings.
func Add(opts AddOptions) (*AddResult, error) {
	if err := docker.EnsureRunning(); err != nil {
		return nil, err
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if err := docker.EnsureInitialized(cfg.NetworkName); err != nil {
		return nil, err
	}

	setup, err := resolveAddSetup(opts)
	if err != nil {
		return nil, err
	}

	if err := writeAddFiles(cfg, setup); err != nil {
		return nil, err
	}

	res := &AddResult{Name: setup.siteName, Domain: setup.domain, Type: setup.typeLabel(), IsLocal: opts.Local}
	if opts.Local {
		res.Warnings = append(res.Warnings, issueLocalCert(setup.siteName, setup.allDomains(), opts.Wildcard)...)
	}
	if opts.Start {
		res.Warnings = append(res.Warnings, startAfterAdd(cfg, setup)...)
	}
	return res, nil
}

// resolveAddSetup detects the project type and assembles + validates the config.
func resolveAddSetup(opts AddOptions) (*addSetup, error) {
	sitePath, err := ResolvePath(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}
	if _, err := os.Stat(sitePath); err != nil {
		return nil, fmt.Errorf("path does not exist: %s", sitePath)
	}

	port := opts.Port
	if port == 0 {
		port = constants.DefaultContainerPort
	}
	s := &addSetup{opts: opts, sitePath: sitePath, port: port}

	if err := detectType(s, opts.TypeOverride); err != nil {
		return nil, err
	}

	// Compose sites need a service selected (and possibly a profile).
	if !s.isStatic && !s.isDockerfile {
		if err := selectComposeService(s, opts.Service, opts.Profile); err != nil {
			return nil, err
		}
	}

	if opts.Domain == "" {
		return nil, fmt.Errorf("domain is required")
	}
	if err := validate.Domain(opts.Domain); err != nil {
		return nil, fmt.Errorf("invalid domain: %w", err)
	}
	s.domain = opts.Domain

	s.siteName = opts.Name
	if s.siteName == "" {
		s.siteName = SanitizeName(opts.Domain)
	}
	if err := validate.SiteName(s.siteName); err != nil {
		return nil, err
	}
	if Exists(s.siteName) && !opts.Force {
		return nil, fmt.Errorf("site %q already exists (set force to overwrite)", s.siteName)
	}

	if opts.Wildcard && !opts.Local {
		return nil, fmt.Errorf("wildcard requires local (Let's Encrypt cannot issue local wildcard certs)")
	}
	aliases, err := normalizeAddAliases(opts.Domain, opts.Aliases)
	if err != nil {
		return nil, err
	}
	s.aliases = aliases

	if opts.InternalHTTP {
		s.listeners = append(s.listeners, constants.ListenerInternal)
	}
	if err := validate.Port(s.port); err != nil {
		return nil, err
	}
	return s, nil
}

// detectType resolves the site type, honouring an explicit override.
func detectType(s *addSetup, override string) error {
	if override != "" {
		switch strings.ToLower(override) {
		case "dockerfile":
			info, err := DetectDockerfileSite(s.sitePath)
			if err != nil || info == nil {
				info = &DockerfileSiteInfo{Port: constants.DockerfileDefaultPort}
			}
			s.isDockerfile = true
			s.dockerfileInfo = info
		case "static":
			s.isStatic = true
		case "compose":
			composePath, err := FindComposeFile(s.sitePath)
			if err != nil {
				return fmt.Errorf("no docker-compose.yml found (required for type=compose)")
			}
			s.composePath = composePath
		default:
			return fmt.Errorf("unknown site type %q — valid types: dockerfile, static, compose", override)
		}
		return nil
	}

	// Auto-detect: compose → Dockerfile → static.
	composePath, err := FindComposeFile(s.sitePath)
	if err != nil && !IsNotFoundError(err) {
		return fmt.Errorf("could not check for docker-compose file: %w", err)
	}
	if err == nil {
		s.composePath = composePath
		return nil
	}
	info, err := DetectDockerfileSite(s.sitePath)
	if err != nil {
		return fmt.Errorf("could not check for Dockerfile: %w", err)
	}
	if info != nil {
		s.isDockerfile = true
		s.dockerfileInfo = info
		return nil
	}
	s.isStatic = true
	return nil
}

// selectComposeService resolves the service (and profile) for a compose site.
func selectComposeService(s *addSetup, service, profile string) error {
	services, err := GetServiceInfos(s.composePath)
	if err != nil {
		return fmt.Errorf("parse compose file: %w", err)
	}
	if len(services) == 0 {
		return fmt.Errorf("no services found in compose file")
	}

	var selected *ServiceInfo
	switch {
	case service != "":
		for i, svc := range services {
			if svc.ContainerName == service || svc.ServiceName == service {
				selected = &services[i]
				break
			}
		}
		if selected == nil {
			return fmt.Errorf("service %q not found in compose file", service)
		}
	case len(services) == 1:
		selected = &services[0]
	default:
		labels := make([]string, len(services))
		for i, svc := range services {
			labels[i] = svc.ContainerName
		}
		return fmt.Errorf("compose file declares %d services (%s); set service to pick one", len(services), strings.Join(labels, ", "))
	}

	if err := validate.ContainerName(selected.ContainerName); err != nil {
		return fmt.Errorf("compose container name: %w", err)
	}
	if err := validate.ContainerName(selected.ServiceName); err != nil {
		return fmt.Errorf("compose service name: %w", err)
	}
	s.serviceName = selected.ContainerName
	s.composeServiceName = selected.ServiceName
	if selected.Port > 0 && s.port == constants.DefaultContainerPort {
		s.port = selected.Port
	}

	switch len(selected.Profiles) {
	case 0:
	case 1:
		s.profile = selected.Profiles[0]
	default:
		if profile == "" {
			return fmt.Errorf("compose service declares %d profiles (%s); set profile to pick one", len(selected.Profiles), strings.Join(selected.Profiles, ", "))
		}
		ok := false
		for _, p := range selected.Profiles {
			if p == profile {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("profile %q is not one of the service's profiles (%s)", profile, strings.Join(selected.Profiles, ", "))
		}
		s.profile = profile
	}
	return nil
}

// writeAddFiles writes metadata.yml and the per-type artifacts.
func writeAddFiles(cfg *config.Config, s *addSetup) error {
	siteType := SiteTypeCompose
	switch {
	case s.isDockerfile:
		siteType = SiteTypeDockerfile
	case s.isStatic:
		siteType = SiteTypeStatic
	}

	port := s.port
	if s.isDockerfile && s.dockerfileInfo != nil {
		port = s.dockerfileInfo.Port
	}

	meta := SiteMetadata{
		Type:               siteType,
		Domains:            s.allDomains(),
		ProjectPath:        s.sitePath,
		ServiceName:        s.serviceName,
		ComposeServiceName: s.composeServiceName,
		Profile:            s.profile,
		Port:               port,
		IsLocal:            s.opts.Local,
		Wildcard:           s.opts.Wildcard,
		NetworkName:        cfg.NetworkName,
		Listeners:          s.listeners,
		SPA:                s.opts.SPA,
		Cache:              s.opts.Cache,
		CORS:               s.opts.CORS,
		Volumes:            s.opts.Volumes,
	}
	if s.isDockerfile && s.dockerfileInfo != nil {
		meta.DockerfilePort = s.dockerfileInfo.Port
		meta.ServiceName = "srv-" + s.siteName + "-app"
	}

	if err := WriteSiteMetadata(s.siteName, meta); err != nil {
		return fmt.Errorf("write site metadata: %w", err)
	}

	switch {
	case s.isDockerfile:
		if err := WriteDockerfileSiteConfig(s.siteName, meta, s.dockerfileInfo, s.opts.Force); err != nil {
			return fmt.Errorf("write Dockerfile site config: %w", err)
		}
	case s.isStatic:
		if err := WriteStaticSiteConfig(s.siteName, meta, s.opts.Force); err != nil {
			return fmt.Errorf("write static site config: %w", err)
		}
	default:
		if err := traefik.WriteSiteRouteConfig(cfg, traefik.SiteRouteConfig{
			Name:        s.siteName,
			Domains:     s.allDomains(),
			ServiceName: s.serviceName,
			Port:        s.port,
			IsLocal:     s.opts.Local,
			Wildcard:    s.opts.Wildcard,
			Listeners:   meta.Listeners,
		}); err != nil {
			return fmt.Errorf("write traefik config: %w", err)
		}
	}
	return nil
}

// issueLocalCert registers DNS for every domain and issues the mkcert cert,
// installing the CA when needed. Best-effort: returns warnings, never errors.
func issueLocalCert(siteName string, domains []string, wildcard bool) (warnings []string) {
	if len(domains) == 0 {
		return nil
	}
	for _, d := range domains {
		if err := traefik.RegisterLocalDomain(d, wildcard); err != nil {
			warnings = append(warnings, fmt.Sprintf("register DNS for %s: %v", d, err))
		}
	}
	if err := traefik.CheckMkcert(); err != nil {
		return append(warnings, fmt.Sprintf("mkcert unavailable, local HTTPS will not work: %v", err))
	}
	if !traefik.IsCAInstalled() {
		if _, err := traefik.InstallCA(); err != nil {
			return append(warnings, fmt.Sprintf("install mkcert CA: %v", err))
		}
	}
	renewed, err := traefik.EnsureLocalCert(siteName, domains, wildcard)
	if err != nil {
		return append(warnings, fmt.Sprintf("generate certificate: %v", err))
	}
	if renewed {
		if err := traefik.UpdateDynamicConfig(); err != nil {
			warnings = append(warnings, fmt.Sprintf("update Traefik config: %v", err))
		}
	}
	return warnings
}

// startAfterAdd brings the new site's containers up. Best-effort warnings.
func startAfterAdd(cfg *config.Config, s *addSetup) (warnings []string) {
	composeDir := s.sitePath
	if s.isStatic || s.isDockerfile {
		composeDir = SiteConfigDir(cfg, s.siteName)
	}
	if err := docker.ComposeUpWithProfile(composeDir, s.profile); err != nil {
		return append(warnings, fmt.Sprintf("start site: %v", err))
	}
	if !s.isStatic && !s.isDockerfile && s.composeServiceName != "" {
		if err := docker.ConnectServiceToNetwork(s.sitePath, s.composeServiceName, cfg.NetworkName); err != nil && !errors.Is(err, docker.ErrServiceNotRunning) {
			warnings = append(warnings, fmt.Sprintf("connect service to traefik network: %v", err))
		}
	}
	return warnings
}

// normalizeAddAliases lowercases, dedupes, validates, and rejects an alias
// equal to the canonical domain.
func normalizeAddAliases(canonical string, aliases []string) ([]string, error) {
	canonical = strings.ToLower(strings.TrimSpace(canonical))
	seen := map[string]bool{canonical: true}
	out := make([]string, 0, len(aliases))
	for _, raw := range aliases {
		a := strings.ToLower(strings.TrimSpace(raw))
		if a == "" {
			continue
		}
		if err := validate.Domain(a); err != nil {
			return nil, fmt.Errorf("invalid alias %q: %w", raw, err)
		}
		if seen[a] {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	return out, nil
}
