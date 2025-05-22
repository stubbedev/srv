// Package docker provides Docker and Docker Compose operations.
package docker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Timeout constants for Docker commands.
const (
	InfoTimeout    = 10 * time.Second // Quick check if Docker is running
	StatusTimeout  = 30 * time.Second // Status checks
	ComposeTimeout = 5 * time.Minute  // Compose operations (can take a while)
)

// Image constants for Docker images used by the application.
const (
	ImageTraefik = "traefik:latest"
	ImageDNS     = "jpillora/dnsmasq:latest"
)

// Container name constants.
const (
	ContainerTraefik = "traefik"
	ContainerDNS     = "srv-dns"
)

// EnsureRunning checks that Docker is available and running.
func EnsureRunning() error {
	ctx, cancel := context.WithTimeout(context.Background(), InfoTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "info")
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("Docker check timed out. Try: docker info\n  Docker may be unresponsive or overloaded")
		}
		return fmt.Errorf("Docker is not running or not installed.\n  Start Docker Desktop or run: sudo systemctl start docker")
	}
	return nil
}

// NetworkExists checks if a docker network exists.
func NetworkExists(name string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), StatusTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "network", "inspect", name)
	return cmd.Run() == nil
}

// CreateNetwork creates a docker network.
func CreateNetwork(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), StatusTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "network", "create", name)
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("docker network create timed out")
		}
		return err
	}
	return nil
}

// ComposeUp runs docker compose up -d in the specified directory.
func ComposeUp(dir string) error {
	return Compose(dir, "up", "-d")
}

// ComposeDown runs docker compose down in the specified directory.
func ComposeDown(dir string) error {
	return Compose(dir, "down")
}

// ComposeStop runs docker compose stop in the specified directory.
func ComposeStop(dir string) error {
	return Compose(dir, "stop")
}

// ComposeRestart runs docker compose restart in the specified directory.
func ComposeRestart(dir string) error {
	return Compose(dir, "restart")
}

// composeArgs prepends --env-file flag if env.site exists in the directory.
func composeArgs(dir string, args []string) []string {
	envSiteFile := filepath.Join(dir, "env.site")
	if _, err := os.Stat(envSiteFile); err == nil {
		args = append([]string{"--env-file", "env.site"}, args...)
	}
	return append([]string{"compose"}, args...)
}

// Compose runs docker compose with given arguments in specified directory.
// Output is attached to stdout/stderr for interactive use.
func Compose(dir string, args ...string) error {
	cmd := exec.Command("docker", composeArgs(dir, args)...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ComposeQuiet runs docker compose without stdout/stderr (for parallel execution).
// Has a timeout to prevent hanging indefinitely.
func ComposeQuiet(dir string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), ComposeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", composeArgs(dir, args)...)
	cmd.Dir = dir
	// Explicitly disconnect stdin to prevent any interactive prompts
	cmd.Stdin = nil
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("docker compose timed out after %v", ComposeTimeout)
	}
	return err
}

// ContainerStatus returns the status of containers in a directory.
// Returns "running", "stopped", or "partial (n/m)".
func ContainerStatus(dir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), StatusTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", composeArgs(dir, []string{"ps", "--format", "{{.Status}}"})...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "stopped"
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var running, total int
	for _, line := range lines {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		total++
		if strings.HasPrefix(line, "Up") {
			running++
		}
	}

	switch {
	case total == 0:
		return "stopped"
	case running == total:
		return "running"
	case running > 0:
		return fmt.Sprintf("partial (%d/%d)", running, total)
	default:
		return "stopped"
	}
}

// Run executes a docker command with the given arguments.
// Output is attached to stdout/stderr for interactive use.
func Run(args ...string) error {
	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunQuiet executes a docker command without attaching stdout/stderr.
// Returns the output and any error.
func RunQuiet(args ...string) ([]byte, error) {
	return exec.Command("docker", args...).Output()
}

// IsContainerRunning checks if a container with the given name is running.
func IsContainerRunning(name string) bool {
	output, err := RunQuiet("inspect", "-f", "{{.State.Running}}", name)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "true"
}

// Pull pulls a docker image.
func Pull(image string) error {
	return Run("pull", image)
}
