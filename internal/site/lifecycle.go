// Package site — lifecycle.go holds headless start/stop/restart for a single
// site, shared by the `srv start|stop|restart` CLI and the MCP lifecycle tools.
// The CLI keeps its --all batch handling and progress UI on top; the core
// container choreography lives here so both surfaces behave identically.
package site

import (
	"errors"
	"fmt"
	"sync"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/traefik"
)

// Runner performs lifecycle operations on already-loaded sites. It is the one
// implementation of start/stop/restart/apply: the CLI (single site, --all and
// `srv install`), the MCP tools, `srv add`, `srv reload --restart` and the
// daemon watcher all go through it, so a daemon-served site — which has no
// containers and no compose directory — can never reach docker compose.
//
// Docker preconditions (engine reachable, srv network present) are checked
// lazily and at most once per Runner: a batch made only of daemon-served sites
// never touches the engine, and a batch of N container sites costs one ping
// and one network lookup rather than N of each. The zero value is ready to
// use, and a Runner is safe for concurrent use by batch workers.
type Runner struct {
	// Quiet detaches compose output. Set it for parallel batches, where
	// several compose processes would otherwise interleave on the terminal.
	Quiet bool
	// OnDockerReady, if set, runs once after the preconditions first pass and
	// before any container is touched (e.g. reconciling the edge config after
	// a binary upgrade). It never runs for a batch without container sites.
	OnDockerReady func()

	engineOnce sync.Once
	engineErr  error

	readyOnce sync.Once
	readyCfg  *config.Config
	readyErr  error
}

// Start brings s up. It regenerates per-site artifacts from metadata first
// (Reload, a cheap no-op when nothing changed) and renews the local cert if it
// is close to expiry, then starts the containers with build forcing an image
// rebuild. For a daemon-served site starting is only rendering its Traefik
// route to the daemon's embedded server; Docker is never involved.
func (r *Runner) Start(s *Site, build bool) error {
	var cfg *config.Config
	if !s.DaemonServed {
		var err error
		if cfg, err = r.ready(); err != nil {
			return err
		}
	}
	// A stopped daemon-served site has no route file while its metadata is
	// unchanged, so the hash short-circuit would leave it unrouted: force.
	res, err := reload(s.Name, s.DaemonServed && !s.routed())
	if err != nil {
		return fmt.Errorf("reload site before start: %w", err)
	}
	// A reload that did work already ran EnsureLocalCert and published the
	// result; only a short-circuited one leaves expiry unchecked.
	if res.Skipped && s.IsLocal && len(s.Domains) > 0 {
		// Best-effort: a renewal failure must not block start.
		if renewed, err := traefik.EnsureLocalCert(s.Name, s.Domains, s.Wildcard); err == nil && renewed {
			_ = traefik.UpdateDynamicConfig()
		}
	}
	if s.DaemonServed {
		return nil
	}
	return r.up(cfg, s, build)
}

// Apply brings s's already-regenerated artifacts live: `compose up` for a
// container site (recreating only what changed), and for a daemon-served site
// re-rendering the route if a stop removed it. Callers run Reload first.
func (r *Runner) Apply(s *Site) error {
	if s.DaemonServed {
		if s.routed() {
			return nil
		}
		if _, err := ForceReload(s.Name); err != nil {
			return fmt.Errorf("apply site: %w", err)
		}
		return nil
	}
	cfg, err := r.ready()
	if err != nil {
		return err
	}
	return r.up(cfg, s, false)
}

// Stop stops s's containers. For a daemon-served site removing the Traefik
// route is the whole stop: its domains 404 exactly like a stopped container's
// vanished labels.
func (r *Runner) Stop(s *Site) error {
	if s.DaemonServed {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		return traefik.RemoveSiteRouteConfig(cfg, s.Name)
	}
	if err := r.engine(); err != nil {
		return err
	}
	if err := r.compose(s, "stop"); err != nil {
		return fmt.Errorf("stop site: %w", err)
	}
	return nil
}

// Restart restarts s after regenerating its artifacts. With build the images
// are rebuilt and the containers recreated, which also redoes the network
// attachments a fresh container lacks. A daemon-served site is re-rendered.
func (r *Runner) Restart(s *Site, build bool) error {
	if s.DaemonServed {
		if _, err := ForceReload(s.Name); err != nil {
			return fmt.Errorf("restart site: %w", err)
		}
		return nil
	}
	cfg, err := r.ready()
	if err != nil {
		return err
	}
	if _, err := Reload(s.Name); err != nil {
		return fmt.Errorf("reload site before restart: %w", err)
	}
	if build {
		return r.up(cfg, s, true)
	}
	if err := r.compose(s, "restart"); err != nil {
		return fmt.Errorf("restart site: %w", err)
	}
	return nil
}

// engine checks once that the container engine answers. Enough for stop,
// which only needs to reach existing containers.
func (r *Runner) engine() error {
	r.engineOnce.Do(func() { r.engineErr = docker.EnsureRunning() })
	return r.engineErr
}

// ready checks once that containers can be started: the engine answers and
// the srv network exists. It returns the config for network attachment.
func (r *Runner) ready() (*config.Config, error) {
	r.readyOnce.Do(func() {
		if r.readyErr = r.engine(); r.readyErr != nil {
			return
		}
		if r.readyCfg, r.readyErr = config.Load(); r.readyErr != nil {
			return
		}
		if r.readyErr = docker.EnsureInitialized(r.readyCfg.NetworkName); r.readyErr != nil {
			return
		}
		if r.OnDockerReady != nil {
			r.OnDockerReady()
		}
	})
	return r.readyCfg, r.readyErr
}

// compose runs a compose command for s's stack, honouring Quiet.
func (r *Runner) compose(s *Site, args ...string) error {
	if r.Quiet {
		return docker.ComposeQuiet(s.ComposeDir, args...)
	}
	return docker.Compose(s.ComposeDir, args...)
}

// up runs `compose up` for a container site and connects it to the networks
// its containers must join: the srv network for a compose site's routed
// service (its compose file is the user's, so srv cannot declare it there),
// and the metadata-listed extra networks for srv-managed sites. Compose-type
// sites are rejected at attach time for extras, since they own their compose
// file and already carry them.
func (r *Runner) up(cfg *config.Config, s *Site, build bool) error {
	if err := r.compose(s, docker.ComposeUpArgs(s.Profile, build)...); err != nil {
		return fmt.Errorf("start site: %w", err)
	}
	if s.Type == SiteTypeCompose {
		if s.ComposeServiceName == "" {
			return nil
		}
		// A service not running is expected when it sits behind a compose
		// profile that was not selected.
		if err := docker.ConnectServiceToNetwork(s.Dir, s.ComposeServiceName, cfg.NetworkName); err != nil && !errors.Is(err, docker.ErrServiceNotRunning) {
			return fmt.Errorf("connect service to network %s: %w", cfg.NetworkName, err)
		}
		return nil
	}
	for _, extra := range s.ExtraNetworks {
		if err := docker.ConnectContainerToNetwork(s.ServiceName, extra, s.ServiceName); err != nil {
			return fmt.Errorf("connect %s to network %s: %w", s.ServiceName, extra, err)
		}
	}
	return nil
}

// StartSite loads the named site and starts it; see Runner.Start.
func StartSite(name string, build bool) error {
	s, err := Require(name)
	if err != nil {
		return err
	}
	return new(Runner).Start(s, build)
}

// StopSite loads the named site and stops it; see Runner.Stop.
func StopSite(name string) error {
	s, err := Require(name)
	if err != nil {
		return err
	}
	return new(Runner).Stop(s)
}

// RestartSite loads the named site and restarts it; see Runner.Restart.
func RestartSite(name string, build bool) error {
	s, err := Require(name)
	if err != nil {
		return err
	}
	return new(Runner).Restart(s, build)
}

// RemoveSite stops a site's containers and deletes all of its derived state:
// Traefik route config, extra-routes config, local cert + DNS registrations,
// and the metadata directory. Shared by `srv remove` and the MCP remove_site
// tool. Per-step failures are returned as warnings; only a failed metadata
// delete (the irreversible step) is returned as an error.
func RemoveSite(name string) (warnings []string, err error) {
	s, err := GetByName(name)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, fmt.Errorf("site %q not found", name)
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	if !s.IsBroken {
		if s.DaemonServed {
			// The route file is the deployment for a daemon-served site; it
			// must go with the site or Traefik keeps routing to the daemon.
			if err := traefik.RemoveSiteRouteConfig(cfg, name); err != nil {
				warnings = append(warnings, fmt.Sprintf("remove traefik config: %v", err))
			}
		} else {
			if err := docker.ComposeDown(s.ComposeDir); err != nil {
				warnings = append(warnings, fmt.Sprintf("stop containers: %v", err))
			}
			if s.Type == SiteTypeCompose {
				if err := traefik.RemoveSiteRouteConfig(cfg, name); err != nil {
					warnings = append(warnings, fmt.Sprintf("remove traefik config: %v", err))
				}
			}
		}
		if err := traefik.RemoveRoutesConfig(cfg, name); err != nil {
			warnings = append(warnings, fmt.Sprintf("remove routes config: %v", err))
		}
	}

	if s.IsLocal && len(s.Domains) > 0 {
		if err := traefik.RemoveLocalCerts(name, s.Domains[0]); err != nil {
			warnings = append(warnings, fmt.Sprintf("remove certificate: %v", err))
		}
		if err := traefik.UpdateDynamicConfig(); err != nil {
			warnings = append(warnings, fmt.Sprintf("update Traefik config: %v", err))
		}
		for _, d := range s.Domains {
			if err := traefik.UnregisterLocalDomain(d); err != nil {
				warnings = append(warnings, fmt.Sprintf("unregister DNS for %s: %v", d, err))
			}
		}
	}

	if err := RemoveSiteMetadata(name); err != nil {
		return warnings, err
	}
	return warnings, nil
}

// Require loads a site by name and rejects missing or broken sites with a
// clear error — the common preamble for every lifecycle op.
func Require(name string) (*Site, error) {
	s, err := getByName(name, false)
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
