// Package cmd — site_add_files.go renders the on-disk artifacts for a new
// site: per-type metadata.yml, generated docker-compose / nginx / Dockerfile
// for srv-managed sites, or Traefik file-provider config for compose sites.
package cmd

import (
	"fmt"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

// setupSiteFiles writes configuration files for the site
// All config is stored in ~/.config/srv - no files are created in the project directory
func setupSiteFiles(cfg *config.Config, setup *siteSetup) error {
	switch {
	case setup.isDockerfile:
		ui.Info("Configuring Dockerfile site: %s", setup.siteName)
	case setup.isStatic:
		ui.Info("Configuring static site: %s", setup.siteName)
	default:
		ui.Info("Configuring site: %s", setup.siteName)
	}

	// Determine site type
	siteType := site.SiteTypeCompose
	switch {
	case setup.isDockerfile:
		siteType = site.SiteTypeDockerfile
	case setup.isStatic:
		siteType = site.SiteTypeStatic
	}

	// Determine canonical port for routing metadata.
	port := setup.port
	if setup.isDockerfile && setup.dockerfileInfo != nil {
		port = setup.dockerfileInfo.Port
	}

	// Build base metadata.
	meta := site.SiteMetadata{
		Type:               siteType,
		Domains:            setup.allDomains(),
		ProjectPath:        setup.sitePath,
		ServiceName:        setup.serviceName,
		ComposeServiceName: setup.composeServiceName,
		Profile:            setup.profile,
		Port:               port,
		IsLocal:            setup.isLocal,
		Wildcard:           setup.wildcard,
		NetworkName:        cfg.NetworkName,
		Listeners:          setup.listeners,
		SPA:                setup.spa,
		Cache:              setup.cache,
		CORS:               setup.cors,
	}

	// Add Dockerfile-specific fields to metadata.
	if setup.isDockerfile && setup.dockerfileInfo != nil {
		meta.DockerfilePort = setup.dockerfileInfo.Port
		meta.ServiceName = "srv-" + setup.siteName + "-app"
	}

	for _, spec := range addFlags.volumes {
		mount, err := ParseVolumeSpec(spec)
		if err != nil {
			return fmt.Errorf("invalid --volume %q: %w", spec, err)
		}
		meta.Volumes = append(meta.Volumes, mount)
	}

	if err := site.WriteSiteMetadata(setup.siteName, meta); err != nil {
		return fmt.Errorf("failed to write site metadata: %w", err)
	}

	switch {
	case setup.isDockerfile:
		// Dockerfile site: generate docker-compose.yml that builds from project Dockerfile.
		if err := site.WriteDockerfileSiteConfig(setup.siteName, meta, setup.dockerfileInfo, addFlags.force); err != nil {
			return fmt.Errorf("failed to write Dockerfile site config: %w", err)
		}
	case setup.isStatic:
		// Static site: generate docker-compose.yml and nginx.conf in config dir.
		if err := site.WriteStaticSiteConfig(setup.siteName, meta, addFlags.force); err != nil {
			return fmt.Errorf("failed to write static site config: %w", err)
		}
	default:
		// Docker-compose site: generate Traefik file provider config.
		routeConfig := traefik.SiteRouteConfig{
			Name:        setup.siteName,
			Domains:     setup.allDomains(),
			ServiceName: setup.serviceName,
			Port:        setup.port,
			IsLocal:     setup.isLocal,
			Wildcard:    setup.wildcard,
			Listeners:   meta.Listeners,
		}
		if err := traefik.WriteSiteRouteConfig(cfg, routeConfig); err != nil {
			return fmt.Errorf("failed to write traefik config: %w", err)
		}
	}

	return nil
}
