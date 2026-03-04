// Package daemon provides a background service that watches Docker events
// and automatically connects containers to the srv network.
package daemon

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	dockerevents "github.com/docker/docker/api/types/events"
	dockerfilters "github.com/docker/docker/api/types/filters"
	dockerclient "github.com/docker/docker/client"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
)

// LogFile is the name of the daemon log file.
const LogFile = "daemon.log"

// refreshCooldown is the minimum interval between automatic container-mapping
// refreshes triggered by untracked container start events.
const refreshCooldown = 5 * time.Second

// Daemon watches Docker events and auto-connects containers to the srv network.
type Daemon struct {
	cfg             *config.Config
	networkName     string
	containers      map[string]string // container name -> site name mapping
	ctx             context.Context
	cancel          context.CancelFunc
	logFile         *os.File
	lastRefreshTime time.Time // guards against refresh storms
}

// New creates a new daemon instance.
func New() (*Daemon, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Daemon{
		cfg:         cfg,
		networkName: cfg.NetworkName,
		containers:  make(map[string]string),
		ctx:         ctx,
		cancel:      cancel,
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
		return fmt.Errorf("daemon service is not installed")
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
	defer logFile.Close()

	d.log("Daemon started, watching for container events on network %s", d.networkName)

	// Build initial container mapping from registered sites
	if err := d.refreshContainerMapping(); err != nil {
		d.log("Warning: failed to load site mappings: %v", err)
	}

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		d.log("Received shutdown signal")
		d.cancel()
	}()

	// Watch Docker events
	return d.watchEvents()
}

// log writes a timestamped message to the log file.
func (d *Daemon) log(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	if d.logFile != nil {
		fmt.Fprintf(d.logFile, "[%s] %s\n", timestamp, msg)
	}
}

// refreshContainerMapping rebuilds the container name to site name mapping.
func (d *Daemon) refreshContainerMapping() error {
	sites, err := site.List()
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

// isDockerAvailable checks if the Docker daemon is reachable via the SDK.
func isDockerAvailable() bool {
	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		return false
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = cli.Ping(ctx)
	return err == nil
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

// runEventLoop runs a single event watching session using the Docker SDK.
func (d *Daemon) runEventLoop() error {
	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}
	defer cli.Close()

	f := dockerfilters.NewArgs(
		dockerfilters.Arg("type", string(dockerevents.ContainerEventType)),
		dockerfilters.Arg("event", "start"),
	)

	eventCh, errCh := cli.Events(d.ctx, dockerevents.ListOptions{Filters: f})

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
func (d *Daemon) handleContainerStart(event dockerevents.Message) {
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
			d.refreshContainerMapping()
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
		if !strings.Contains(err.Error(), constants.ErrAlreadyExists) &&
			!strings.Contains(err.Error(), constants.ErrEndpointExists) {
			d.log("Failed to connect %s to network: %v", containerName, err)
		}
	} else {
		d.log("Successfully connected %s to network %s", containerName, d.networkName)
	}
}
