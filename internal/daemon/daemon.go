// Package daemon provides a background service that watches Docker events
// and automatically connects containers to the srv network.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/dnsd"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/httpd"
	"github.com/stubbedev/srv/internal/logfile"
	"github.com/stubbedev/srv/internal/ops"
	"github.com/stubbedev/srv/internal/proxy"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/traefik"
)

// LogFile is the name of the daemon log file.
const LogFile = "daemon.log"

// StderrLogFile receives the daemon process's own stdout/stderr under
// launchd (Go runtime panics). It is not rotated: nothing writes it in normal
// operation.
const StderrLogFile = "daemon.stderr.log"

// refreshCooldown is the minimum interval between automatic container-mapping
// refreshes triggered by untracked container start events.
const refreshCooldown = 5 * time.Second

// Daemon watches Docker events and auto-connects containers to the srv network.
type Daemon struct {
	cfg         *config.Config
	networkName string
	// containers maps container name -> site name. Swapped whole on refresh
	// and read lock-free by the event loop; never mutate a loaded map.
	containers atomic.Pointer[map[string]string]
	ctx        context.Context
	cancel     context.CancelFunc
	// logFile is the daemon log. Its Write is safe for concurrent use, so the
	// signal, watcher, event and HTTP goroutines share it without a lock here.
	logFile *logfile.Writer
	// lastRefreshTime throttles miss-driven mapping refreshes when the
	// metadata watcher is off; sitesDirStamp records the sites directory
	// state the mapping was last built from.
	lastRefreshTime time.Time
	sitesDirStamp   time.Time
	// static hosts the embedded static file server (daemon-served sites) so
	// the metadata watcher can refresh its host table. Nil until bound.
	static atomic.Pointer[httpd.Server]
	// dns hosts the embedded DNS server so the structured zone source can
	// push fresh snapshots as the server is (re)bound. Nil until bound.
	dns atomic.Pointer[dnsd.Server]
	// watching is set once the metadata watcher runs and pushes mapping
	// refreshes itself.
	watching atomic.Bool
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

	d := &Daemon{
		cfg:           cfg,
		networkName:   cfg.NetworkName,
		ctx:           ctx,
		cancel:        cancel,
		WatchMetadata: true,
	}
	d.setContainers(map[string]string{})
	return d, nil
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
	// One daemon per srv root: the lock holder is the single process serving
	// the embedded DNS and static servers for every site and container.
	lock, err := acquireDaemonLock(d.cfg)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	// Open log file
	logPath := LogPath(d.cfg)
	logFile, err := logfile.Open(logPath, constants.FilePermDefault)
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

	// Daemon-served routes are rendered by this binary's rules (the compress
	// middleware, the embedded server's address); re-render them once per
	// start so an upgrade applies to existing sites without a manual reload.
	d.rerenderDaemonRoutes()

	// Embedded static file server: daemon-served sites ("srv add --daemon")
	// are served straight from this process — one listener multiplexes every
	// one of them by Host header, the way the embedded DNS server serves
	// every local domain. Same contract: never fatal, retried on a timer.
	d.startStaticServer()

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

// rerenderDaemonRoutes force-reloads every daemon-served site. Best-effort:
// failures are logged and the previous route file keeps serving.
func (d *Daemon) rerenderDaemonRoutes() {
	sites, err := listSites()
	if err != nil {
		return
	}
	for _, s := range sites {
		if !s.DaemonServed || s.IsBroken {
			continue
		}
		if _, err := site.ForceReload(s.Name); err != nil {
			d.log("Route refresh %s: %v", s.Name, err)
		}
	}
}

// startEmbeddedDNS binds the loopback at the unprivileged embedded port
// (constants.PortDNS — the daemon is a user service and cannot bind 53) and
// serves the local-domain zones: a snapshot built from the structured config
// (see dns_zones.go) over the generated zone files as the fallback layer. It
// never fails the daemon: DNS is one of its jobs, not its reason to run. A
// bind failure (port held by something else) is retried on a slow timer so
// an upgrade converges without a daemon restart.
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

		d.startDNSZoneSource()

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
			d.dns.Store(server)
			d.refreshDNSZones()

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

// startStaticServer binds the daemon's embedded static file server on the
// host loopback (constants.PortStatic — unprivileged, like the DNS port) and
// serves every daemon-served static site from it. Traefik terminates TLS for
// the site's domains and forwards here; the daemon holds the files.
func (d *Daemon) startStaticServer() {
	if d.cfg == nil || d.cfg.Root == "" {
		return
	}
	go func() {
		addr := net.JoinHostPort(constants.LocalhostIP, constants.PortStaticStr)
		for d.ctx.Err() == nil {
			srv := httpd.New(addr)
			if d.logFile != nil {
				zl := zerolog.New(d.logFile).With().Timestamp().Logger()
				srv.Logger = &zl
			}
			if err := srv.Reload(); err != nil {
				d.log("Static server target refresh failed: %v", err)
			}
			d.static.Store(srv)
			d.log("Embedded static server listening on %s", addr)

			served := make(chan error, 1)
			go func() { served <- srv.Serve() }()

			select {
			case <-d.ctx.Done():
				srv.Shutdown()
				return
			case err := <-served:
				if d.ctx.Err() != nil {
					return
				}
				// Transient failure (bind conflict, EMFILE): back off and
				// rebind so an upgrade converges without a daemon restart.
				d.log("Embedded static server stopped (%v); restarting in 10s", err)
			}
			select {
			case <-d.ctx.Done():
				return
			case <-time.After(10 * time.Second):
			}
		}
	}()
}

// refreshStaticSites re-reads the daemon-served host table after site
// metadata changed. Cheap: one directory scan + the site's metadata.ymls.
func (d *Daemon) refreshStaticSites() {
	if srv := d.static.Load(); srv != nil {
		if err := srv.Reload(); err != nil {
			d.log("Static server reload: %v", err)
		}
	}
}

// log writes a timestamped message to the log file. Daemon messages are rare
// and matter when something goes wrong, so each is flushed at once; only the
// high-volume access log rides the writer's flush interval.
func (d *Daemon) log(format string, args ...any) {
	if d.logFile == nil {
		return
	}
	// One Write per line: the writer keeps each line contiguous on disk.
	line := fmt.Sprintf("[%s] %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
	_, _ = d.logFile.Write([]byte(line))
	_ = d.logFile.Flush()
}

// refreshContainerMapping rebuilds the container name to site name mapping
// and swaps it in. The metadata watcher calls it whenever a site is added,
// removed or edited, so the Docker event path only has to read the map.
// It logs only when the mapping changed.
func (d *Daemon) refreshContainerMapping() error {
	sites, err := listSites()
	if err != nil {
		return err
	}

	next := make(map[string]string)
	for _, s := range sites {
		if s.ServiceName != "" && s.Type == site.SiteTypeCompose {
			next[s.ServiceName] = s.Name
		}
	}
	if prev := d.containers.Load(); prev == nil || !maps.Equal(*prev, next) {
		d.log("Loaded %d container mappings", len(next))
	}
	d.setContainers(next)
	return nil
}

// listSites reads every registered site; tests swap it to count disk scans.
var listSites = site.ListBasic

// setContainers installs a container mapping. The map must not be mutated
// afterwards: the event loop reads it without a lock.
func (d *Daemon) setContainers(m map[string]string) { d.containers.Store(&m) }

// containerSite looks a container up in the current mapping.
func (d *Daemon) containerSite(name string) (string, bool) {
	m := d.containers.Load()
	if m == nil {
		return "", false
	}
	site, ok := (*m)[name]
	return site, ok
}

// mappingMayBeStale reports whether a container the mapping does not know
// could belong to a site the mapping has not seen yet. With the metadata
// watcher running, edits and removals are pushed; only a brand-new site
// directory (its metadata may land before the watch on it exists) can be
// missed, and a new directory changes the sites directory's mtime — one
// stat, instead of re-reading every site's metadata for every foreign
// container start. Without the watcher, fall back to a throttled refresh.
func (d *Daemon) mappingMayBeStale() bool {
	if d.watching.Load() {
		st, err := os.Stat(d.cfg.SitesDir)
		if err != nil || st.ModTime().Equal(d.sitesDirStamp) {
			return false
		}
		d.sitesDirStamp = st.ModTime()
		return true
	}
	if time.Since(d.lastRefreshTime) < refreshCooldown {
		return false
	}
	d.lastRefreshTime = time.Now()
	return true
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

// watchDockerEvents opens the container event stream. Tests swap it to feed
// the event loop without a Docker daemon.
var watchDockerEvents = docker.WatchEvents

// runEventLoop runs a single event watching session against the daemon API.
func (d *Daemon) runEventLoop() error {
	// Resolving the engine exports DOCKER_HOST, which the event client reads.
	eng := ops.Engine()
	eventCh, errCh, err := watchDockerEvents(d.ctx)
	if err != nil {
		return fmt.Errorf("failed to create %s client: %w", eng.Name, err)
	}

	for {
		select {
		case <-d.ctx.Done():
			return nil
		case err := <-errCh:
			return fmt.Errorf("error reading Docker events: %w", err)
		case event, ok := <-eventCh:
			if !ok {
				// The stream ended (engine restart, socket closed). A closed
				// channel yields zero events forever, so reading on would spin
				// this loop at 100% CPU; return and let watchEvents reconnect.
				// The producer sends its error before closing, so prefer it.
				select {
				case err := <-errCh:
					return fmt.Errorf("error reading Docker events: %w", err)
				default:
					return docker.ErrEventStreamClosed
				}
			}
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

	// Check if this container is one we're tracking. Most starts on a busy
	// host are foreign containers; they must cost a map probe, not disk I/O.
	siteName, tracked := d.containerSite(containerName)
	if !tracked {
		if !d.mappingMayBeStale() {
			return
		}
		_ = d.refreshContainerMapping()
		if siteName, tracked = d.containerSite(containerName); !tracked {
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
