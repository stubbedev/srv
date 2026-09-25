// Package daemon provides a background service that watches Docker events
// and automatically connects containers to the srv network.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/dnsd"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/ops"
	"github.com/stubbedev/srv/internal/proxy"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/traefik"
)

// LogFile is the name of the daemon log file.
const LogFile = "daemon.log"

// refreshCooldown is the minimum interval between automatic container-mapping
// refreshes triggered by untracked container start events.
const refreshCooldown = 5 * time.Second

// Daemon watches Docker events and auto-connects containers to the srv network.
type Daemon struct {
	cfg         *config.Config
	networkName string
	containers  map[string]string // container name -> site name mapping
	ctx         context.Context
	cancel      context.CancelFunc
	logMu       sync.Mutex // serialises concurrent log() writes from the
	// signal, metadata-watcher, and Docker-event goroutines.
	logFile         *os.File
	lastRefreshTime time.Time // guards against refresh storms
	// WatchMetadata controls whether the daemon also watches site metadata.yml
	// files and hot-reloads them. Set via `srv daemon start --no-watch=false`.
	WatchMetadata bool
}

// New creates a new daemon instance.
func New() (*Daemon, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Daemon{
		cfg:           cfg,
		networkName:   cfg.NetworkName,
		containers:    make(map[string]string),
		ctx:           ctx,
		cancel:        cancel,
		WatchMetadata: true,
	}, nil
}

// LogPath returns the path to the log file.
func LogPath(cfg *config.Config) string {
	return filepath.Join(cfg.Root, LogFile)
}

// IsRunning checks if the daemon is currently running via the service manager.
func IsRunning() bool {
	status, err := ServiceStatus()
	if err != nil {
		return false
	}
	return status == "active" || status == "running"
}

// Stop stops the running daemon via the service manager.
func Stop() error {
	if !IsInstalled() {
		return errors.New("daemon service is not installed")
	}
	return stopService()
}

// Run starts the daemon and blocks until stopped.
func (d *Daemon) Run() error {
	// Open log file
	logPath := LogPath(d.cfg)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, constants.FilePermDefault)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	d.logFile = logFile
	defer func() { _ = logFile.Close() }()

	d.log("Daemon started, watching for container events on network %s", d.networkName)

	// Build initial container mapping from registered sites
	if err := d.refreshContainerMapping(); err != nil {
		d.log("Warning: failed to load site mappings: %v", err)
	}

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	go func() {
		select {
		case <-sigChan:
			d.log("Received shutdown signal")
			d.cancel()
		case <-d.ctx.Done():
		}
	}()

	// Watch metadata.yml writes (P3 hot-reload) unless disabled.
	if d.WatchMetadata {
		if _, err := d.startMetadataWatcher(); err != nil {
			d.log("Metadata watcher disabled: %v", err)
		}
	} else {
		d.log("Metadata watcher disabled by --no-watch")
	}

	// Embedded DNS: serve the local domains from the generated zone files on
	// the host loopback. The dnsmasq container this replaces is long gone from
	// the stack; best-effort clean it up for installs upgraded from one, so
	// the port it still holds frees up for this server.
	d.startEmbeddedDNS()

	// Retired-hop cleanup: proxies created with --fallback before the native
	// Traefik failover carry either a daemon-hosted listener port or an nginx
	// sidecar. Re-render them natively and remove the hop, best-effort, once
	// per daemon start; failures are logged and the next start retries.
	d.migrateLegacyFallbacks()

	// Watch Docker events
	return d.watchEvents()
}

// migrateLegacyFallbacks re-renders proxies whose fallback still uses a
// retired hop. Never fails the daemon: per-proxy warnings land in the log.
func (d *Daemon) migrateLegacyFallbacks() {
	entries, err := os.ReadDir(filepath.Join(d.cfg.Root, constants.ProxiesSubdir))
	if err != nil {
		return // no proxies dir yet — nothing to migrate
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}
		meta, err := proxy.Read(strings.TrimSuffix(entry.Name(), ".yml"))
		if err != nil || meta == nil {
			continue
		}
		for _, w := range proxy.MigrateLegacyFallback(d.cfg, meta) {
			d.log("Fallback migration: %s", w)
		}
	}
}

// startEmbeddedDNS binds the loopback at the unprivileged embedded port
// (constants.PortDNS — the daemon is a user service and cannot bind 53) and
// serves the local-domain zones. It never fails the daemon: DNS is one of its
// jobs, not its reason to run. A bind failure (port held by something else)
// is retried on a slow timer so an upgrade converges without a daemon restart.
func (d *Daemon) startEmbeddedDNS() {
	if d.cfg == nil || d.cfg.TraefikDir == "" {
		return
	}
	go func() {
		// Refresh the system resolver routing config: installs upgraded from
		// the dnsmasq container era point at the old port 53, and nothing else
		// rewrites this until the next domain add. Idempotent — a no-op when
		// the on-disk config already matches. Needs sudo for /etc; failure is
		// logged, not fatal, and the next `srv dns setup` repairs it.
		if err := traefik.SetupDNS(); err != nil {
			d.log("DNS routing refresh failed (run 'srv dns setup'): %v", err)
		}

		confPath := filepath.Join(d.cfg.TraefikDir, constants.DnsmasqConfFile)
		hostsPath := filepath.Join(d.cfg.TraefikDir, constants.DnsmasqHostsDir, constants.DnsmasqHostsFile)

		// The legacy dnsmasq container holds the old port on installs that
		// predate the embedded server. It is no longer in the compose file, so
		// nothing restarts it — remove it once, best-effort.
		_ = docker.RemoveContainer("srv_dns")

		for d.ctx.Err() == nil {
			server, err := dnsd.New(constants.LocalhostIP, constants.PortDNS, confPath, hostsPath)
			if err != nil {
				d.log("Embedded DNS unavailable (%v); retrying in 30s", err)
				select {
				case <-d.ctx.Done():
					return
				case <-time.After(30 * time.Second):
				}
				continue
			}
			d.log("Embedded DNS listening on %s (zones: %s)", server.Addr(), d.cfg.TraefikDir)

			watchDone := make(chan struct{})
			go func() {
				defer close(watchDone)
				if err := server.Watch(); err != nil {
					d.log("DNS zone watcher stopped: %v", err)
				}
			}()
			_ = server.Serve()
			<-watchDone

			if d.ctx.Err() != nil {
				return
			}
			// Serve returned while the daemon lives: transient failure. Back
			// off briefly and rebind.
			d.log("Embedded DNS stopped; restarting in 5s")
			select {
			case <-d.ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}()
}

// log writes a timestamped message to the log file.
func (d *Daemon) log(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	d.logMu.Lock()
	defer d.logMu.Unlock()
	if d.logFile != nil {
		_, _ = fmt.Fprintf(d.logFile, "[%s] %s\n", timestamp, msg)
	}
}

// refreshContainerMapping rebuilds the container name to site name mapping.
func (d *Daemon) refreshContainerMapping() error {
	sites, err := site.ListBasic()
	if err != nil {
		return err
	}

	d.containers = make(map[string]string)
	for _, s := range sites {
		if s.ServiceName != "" && s.Type == site.SiteTypeCompose {
			d.containers[s.ServiceName] = s.Name
		}
	}

	d.log("Loaded %d container mappings", len(d.containers))
	return nil
}

// isDockerAvailable checks if the Docker daemon is reachable. Tests swap
// the docker package's SDK client factory to control the answer.
var isDockerAvailable = func() bool {
	return docker.EnsureRunning() == nil
}

// waitForDocker waits for Docker daemon to become available with exponential backoff.
func (d *Daemon) waitForDocker() error {
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-d.ctx.Done():
			return d.ctx.Err()
		default:
		}

		if isDockerAvailable() {
			return nil
		}

		d.log("Docker daemon not running, retrying in %v...", backoff)

		select {
		case <-d.ctx.Done():
			return d.ctx.Err()
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// watchEvents watches Docker events and handles container starts.
func (d *Daemon) watchEvents() error {
	for {
		select {
		case <-d.ctx.Done():
			return nil
		default:
		}

		// Wait for Docker to be available
		if err := d.waitForDocker(); err != nil {
			return err
		}

		d.log("Docker is available, starting event watcher")

		err := d.runEventLoop()
		if err != nil && d.ctx.Err() == nil {
			d.log("Event loop error: %v, restarting in 5s...", err)
			select {
			case <-d.ctx.Done():
				return nil
			case <-time.After(5 * time.Second):
			}
		}
	}
}

// runEventLoop runs a single event watching session against the daemon API.
func (d *Daemon) runEventLoop() error {
	// Resolving the engine exports DOCKER_HOST, which the event client reads.
	eng := ops.Engine()
	eventCh, errCh, err := docker.WatchEvents(d.ctx)
	if err != nil {
		return fmt.Errorf("failed to create %s client: %w", eng.Name, err)
	}

	for {
		select {
		case <-d.ctx.Done():
			return nil
		case err := <-errCh:
			return fmt.Errorf("error reading Docker events: %w", err)
		case event := <-eventCh:
			d.handleContainerStart(event)
		}
	}
}

// handleContainerStart processes a container start event.
func (d *Daemon) handleContainerStart(event docker.Event) {
	containerName := event.Actor.Attributes["name"]
	if containerName == "" {
		return
	}

	// Check if this container is one we're tracking
	siteName, tracked := d.containers[containerName]
	if !tracked {
		// Refresh mappings in case a new site was added, but throttle to avoid
		// hammering disk I/O on busy systems with many non-srv containers.
		if time.Since(d.lastRefreshTime) >= refreshCooldown {
			_ = d.refreshContainerMapping()
			d.lastRefreshTime = time.Now()
		}
		siteName, tracked = d.containers[containerName]
		if !tracked {
			return
		}
	}

	d.log("Container %s started (site: %s), connecting to network %s", containerName, siteName, d.networkName)

	// Connect the container to our network
	if err := docker.ConnectContainerToNetwork(containerName, d.networkName, containerName); err != nil {
		// docker.ConnectContainerToNetwork already swallows "already connected"
		// conflicts; anything that reaches us here is a real failure worth logging.
		if !docker.IsConflict(err) {
			d.log("Failed to connect %s to network: %v", containerName, err)
		}
	} else {
		d.log("Successfully connected %s to network %s", containerName, d.networkName)
	}

	// Connect any extra networks the site's metadata lists, best-effort: a
	// missing external network logs but must not stop the primary connect
	// from having happened.
	if meta, err := site.ReadSiteMetadata(siteName); err == nil && meta != nil {
		for _, extra := range meta.ExtraNetworks {
			if extra == d.networkName {
				continue
			}
			if err := docker.ConnectContainerToNetwork(containerName, extra, containerName); err != nil && !docker.IsConflict(err) {
				d.log("Failed to connect %s to extra network %s: %v", containerName, extra, err)
			}
		}
	}
}
