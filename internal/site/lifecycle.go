// Package site — lifecycle.go holds headless start/stop/restart for a single
// site, shared by the `srv start|stop|restart` CLI and the MCP lifecycle tools.
// The CLI keeps its --all batch handling and progress UI on top; the core
// container choreography lives here so both surfaces behave identically.
package site

import (
	"errors"
	"fmt"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/traefik"
)

// StartSite brings a single site's containers up. It ensures Docker + the srv
// network are ready, renews the local cert if needed, regenerates per-site
// artifacts (Reload), then `docker compose up` (with --build when build=true)
// and connects a compose service to the srv network.
func StartSite(name string, build bool) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := docker.EnsureInitialized(cfg.NetworkName); err != nil {
		return err
	}
	s, err := requireSite(name)
	if err != nil {
		return err
	}

	if s.IsLocal && len(s.Domains) > 0 {
		// Best-effort: a renewal failure should not block start.
		_, _ = traefik.EnsureLocalCert(s.Name, s.Domains, s.Wildcard)
	}
	if _, err := Reload(s.Name); err != nil {
		return fmt.Errorf("reload site before start: %w", err)
	}

	if build {
		if err := docker.ComposeUpBuildWithProfile(s.ComposeDir, s.Profile); err != nil {
			return fmt.Errorf("start site: %w", err)
		}
	} else if err := docker.ComposeUpWithProfile(s.ComposeDir, s.Profile); err != nil {
		return fmt.Errorf("start site: %w", err)
	}

	if s.Type == SiteTypeCompose && s.ComposeServiceName != "" {
		if err := docker.ConnectServiceToNetwork(s.Dir, s.ComposeServiceName, cfg.NetworkName); err != nil && !errors.Is(err, docker.ErrServiceNotRunning) {
			return fmt.Errorf("connect service to network: %w", err)
		}
	}
	return nil
}

// StopSite stops a single site's containers.
func StopSite(name string) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}
	s, err := requireSite(name)
	if err != nil {
		return err
	}
	if err := docker.ComposeStop(s.ComposeDir); err != nil {
		return fmt.Errorf("stop site: %w", err)
	}
	return nil
}

// RestartSite restarts a single site's containers, regenerating artifacts first.
func RestartSite(name string, build bool) error {
	if err := docker.EnsureRunning(); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := docker.EnsureInitialized(cfg.NetworkName); err != nil {
		return err
	}
	s, err := requireSite(name)
	if err != nil {
		return err
	}
	if _, err := Reload(s.Name); err != nil {
		return fmt.Errorf("reload site before restart: %w", err)
	}
	if build {
		if err := docker.ComposeUpBuildWithProfile(s.ComposeDir, s.Profile); err != nil {
			return fmt.Errorf("rebuild and restart site: %w", err)
		}
	} else if err := docker.ComposeRestart(s.ComposeDir); err != nil {
		return fmt.Errorf("restart site: %w", err)
	}
	return nil
}

// requireSite loads a site by name and rejects missing or broken sites with a
// clear error — the common preamble for every lifecycle op.
func requireSite(name string) (*Site, error) {
	s, err := GetByName(name)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, fmt.Errorf("site %q not found", name)
	}
	if s.IsBroken {
		return nil, fmt.Errorf("site %q is broken (target directory missing)", s.Name)
	}
	return s, nil
}
