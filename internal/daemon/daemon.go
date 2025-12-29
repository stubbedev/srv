// Package daemon provides a background service that watches Docker events
// and automatically connects containers to the srv network.
package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
)

// LogFile is the name of the daemon log file.
const LogFile = "daemon.log"

// dockerEvent represents a Docker event from the event stream.
type dockerEvent struct {
	Status string `json:"status"`
	ID     string `json:"id"`
	Type   string `json:"Type"`
	Action string `json:"Action"`
	Actor  struct {
		ID         string            `json:"ID"`
		Attributes map[string]string `json:"Attributes"`
	} `json:"Actor"`
}

// Daemon watches Docker events and auto-connects containers to the srv network.
type Daemon struct {
	cfg         *config.Config
	networkName string
	containers  map[string]string // container name -> site name mapping
	ctx         context.Context
	cancel      context.CancelFunc
	logFile     *os.File
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
	// Check if Docker is installed before doing anything
	if !isDockerInstalled() {
		// Uninstall the service since Docker isn't available
		if IsInstalled() {
			Uninstall()
		}
		return fmt.Errorf("docker is not installed, daemon service uninstalled")
	}

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

// isDockerInstalled checks if the docker binary exists on the system.
func isDockerInstalled() bool {
	_, err := exec.LookPath("docker")
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

		// Check if Docker daemon is running
		cmd := exec.CommandContext(d.ctx, "docker", "info")
		if err := cmd.Run(); err == nil {
			return nil
		}

		d.log("Docker daemon not running, retrying in %v...", backoff)

		select {
		case <-d.ctx.Done():
			return d.ctx.Err()
		case <-time.After(backoff):
		}

		// Exponential backoff with max
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

// runEventLoop runs a single event watching session.
func (d *Daemon) runEventLoop() error {
	// Use docker events with JSON format, filtering for container start events
	cmd := exec.CommandContext(d.ctx, "docker", "events",
		"--format", "{{json .}}",
		"--filter", "type=container",
		"--filter", "event=start",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start docker events: %w", err)
	}

	// Create a done channel for the scanner goroutine
	eventChan := make(chan dockerEvent)
	errChan := make(chan error, 1)

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			var event dockerEvent
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				d.log("Failed to parse event: %v", err)
				continue
			}
			select {
			case eventChan <- event:
			case <-d.ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			errChan <- err
		}
		close(eventChan)
	}()

	// Process events until context is cancelled or error occurs
	for {
		select {
		case <-d.ctx.Done():
			cmd.Process.Kill()
			cmd.Wait()
			return nil
		case err := <-errChan:
			cmd.Process.Kill()
			cmd.Wait()
			return fmt.Errorf("error reading events: %w", err)
		case event, ok := <-eventChan:
			if !ok {
				// Channel closed, docker events ended
				return cmd.Wait()
			}
			d.handleContainerStart(event)
		}
	}
}

// handleContainerStart processes a container start event.
func (d *Daemon) handleContainerStart(event dockerEvent) {
	containerName := event.Actor.Attributes["name"]
	if containerName == "" {
		return
	}

	// Check if this container is one we're tracking
	siteName, tracked := d.containers[containerName]
	if !tracked {
		// Refresh mappings in case a new site was added
		d.refreshContainerMapping()
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
