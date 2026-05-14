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
	case setup.isPHP:
		ui.Info("Configuring PHP site: %s", setup.siteName)
	case setup.isNode:
		ui.Info("Configuring Node.js site: %s", setup.siteName)
	case setup.isRuby:
		ui.Info("Configuring Ruby site: %s", setup.siteName)
	case setup.isPython:
		ui.Info("Configuring Python site: %s", setup.siteName)
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
	case setup.isPHP:
		siteType = site.SiteTypePHP
	case setup.isNode:
		siteType = site.SiteTypeNode
	case setup.isRuby:
		siteType = site.SiteTypeRuby
	case setup.isPython:
		siteType = site.SiteTypePython
	case setup.isDockerfile:
		siteType = site.SiteTypeDockerfile
	case setup.isStatic:
		siteType = site.SiteTypeStatic
	}

	// Determine canonical port for routing metadata.
	port := setup.port
	switch {
	case setup.isNode && setup.nodeInfo != nil:
		port = setup.nodeInfo.Port
	case setup.isRuby && setup.rubyInfo != nil:
		port = setup.rubyInfo.Port
	case setup.isPython && setup.pythonInfo != nil:
		port = setup.pythonInfo.Port
	case setup.isDockerfile && setup.dockerfileInfo != nil:
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
		Limits:             setup.limits,
		SPA:                setup.spa,
		Cache:              setup.cache,
		CORS:               setup.cors,
	}

	// Add PHP-specific fields to metadata.
	if setup.isPHP && setup.phpInfo != nil {
		meta.PHPVersion = setup.phpInfo.PHPVersion
		meta.PHPExtensions = setup.phpInfo.Extensions
		meta.PHPFramework = setup.phpInfo.Framework
		meta.DocumentRoot = setup.phpInfo.DocumentRoot
	}

	// Add Node.js-specific fields to metadata.
	if setup.isNode && setup.nodeInfo != nil {
		meta.NodeRuntime = setup.nodeInfo.Runtime
		meta.NodePackageManager = setup.nodeInfo.PackageManager
		meta.NodeVersion = setup.nodeInfo.NodeVersion
		meta.NodeFramework = setup.nodeInfo.Framework
		meta.NodeStartCmd = setup.nodeInfo.StartCmd
		meta.ServiceName = "srv-" + setup.siteName + "-node"
	}

	// Add Ruby-specific fields to metadata.
	if setup.isRuby && setup.rubyInfo != nil {
		meta.RubyVersion = setup.rubyInfo.RubyVersion
		meta.RubyFramework = setup.rubyInfo.Framework
		meta.RubyStartCmd = setup.rubyInfo.StartCmd
		meta.ServiceName = "srv-" + setup.siteName + "-app"
	}

	// Add Python-specific fields to metadata.
	if setup.isPython && setup.pythonInfo != nil {
		meta.PythonVersion = setup.pythonInfo.PythonVersion
		meta.PythonFramework = setup.pythonInfo.Framework
		meta.PythonStartCmd = setup.pythonInfo.StartCmd
		meta.ServiceName = "srv-" + setup.siteName + "-app"
	}

	// Add Dockerfile-specific fields to metadata.
	if setup.isDockerfile && setup.dockerfileInfo != nil {
		meta.DockerfilePort = setup.dockerfileInfo.Port
		meta.ServiceName = "srv-" + setup.siteName + "-app"
	}

	if err := site.WriteSiteMetadata(setup.siteName, meta); err != nil {
		return fmt.Errorf("failed to write site metadata: %w", err)
	}

	switch {
	case setup.isPHP:
		// PHP site: generate Dockerfile, nginx.conf, and docker-compose.yml.
		if err := site.WritePHPSiteConfig(setup.siteName, meta, setup.phpInfo, addFlags.force); err != nil {
			return fmt.Errorf("failed to write PHP site config: %w", err)
		}
	case setup.isNode:
		// Node.js site: generate docker-compose.yml.
		if err := site.WriteNodeSiteConfig(setup.siteName, meta, setup.nodeInfo, addFlags.force); err != nil {
			return fmt.Errorf("failed to write Node site config: %w", err)
		}
	case setup.isRuby:
		// Ruby site: generate docker-compose.yml.
		if err := site.WriteRubySiteConfig(setup.siteName, meta, setup.rubyInfo, addFlags.force); err != nil {
			return fmt.Errorf("failed to write Ruby site config: %w", err)
		}
	case setup.isPython:
		// Python site: generate docker-compose.yml.
		if err := site.WritePythonSiteConfig(setup.siteName, meta, setup.pythonInfo, addFlags.force); err != nil {
			return fmt.Errorf("failed to write Python site config: %w", err)
		}
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
