// Package docker provides Docker and Docker Compose operations.
package docker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/stubbedev/srv/internal/constants"
)

// Timeout constants for Docker commands.
// These values balance responsiveness with allowing time for slower operations.
const (
	// InfoTimeout is for quick checks like "docker info" to verify Docker is running.
	InfoTimeout = 10 * time.Second
	// StatusTimeout is for status checks like inspecting containers or networks.
	StatusTimeout = 30 * time.Second
	// ComposeTimeout is for compose operations which can take several minutes
	// especially when pulling images or building containers.
	ComposeTimeout = 5 * time.Minute
)

// Image constants for Docker images used by the application.
const (
	// ImageTraefik is the Traefik reverse proxy image used for routing.
	ImageTraefik = "traefik:latest"
	// ImageDNS is the dnsmasq image used for local DNS resolution of .test domains.
	ImageDNS = "jpillora/dnsmasq:latest"
)

// Container name constants.
const (
	// ContainerTraefik is the name of the Traefik container managed by srv.
	ContainerTraefik = "srv_proxy"
	// ContainerDNS is the name of the DNS container managed by srv.
	ContainerDNS = "srv_dns"
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
// Uses --remove-orphans to clean up stale containers that may reference non-existent networks.
func ComposeUp(dir string) error {
	return Compose(dir, "up", "-d", "--remove-orphans")
}

// ComposeUpWithProfile runs docker compose up -d with a specific profile.
// Uses --remove-orphans to clean up stale containers that may reference non-existent networks.
func ComposeUpWithProfile(dir, profile string) error {
	if profile == "" {
		return ComposeUp(dir)
	}
	return Compose(dir, "--profile", profile, "up", "-d", "--remove-orphans")
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

// Compose runs docker compose with given arguments in specified directory.
// Output is attached to stdout/stderr for interactive use.
func Compose(dir string, args ...string) error {
	cmd := exec.Command("docker", append([]string{"compose"}, args...)...)
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

	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose"}, args...)...)
	cmd.Dir = dir
	// Explicitly disconnect stdin to prevent any interactive prompts
	cmd.Stdin = nil
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("docker compose timed out after %v", ComposeTimeout)
	}
	return err
}

// ComposeQuietWithProfile runs docker compose with a profile without stdout/stderr.
func ComposeQuietWithProfile(dir, profile string, args ...string) error {
	if profile == "" {
		return ComposeQuiet(dir, args...)
	}
	profileArgs := append([]string{"--profile", profile}, args...)
	return ComposeQuiet(dir, profileArgs...)
}

// ContainerStatus returns the status of containers in a directory.
// Returns "running", "stopped", or "partial (n/m)".
func ContainerStatus(dir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), StatusTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "compose", "ps", "--format", constants.ComposeStatusFormat)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return constants.StatusStopped
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var running, total int
	for _, line := range lines {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		total++
		if strings.HasPrefix(line, constants.StatusPrefixUp) {
			running++
		}
	}

	switch {
	case total == 0:
		return constants.StatusStopped
	case running == total:
		return constants.StatusRunning
	case running > 0:
		return fmt.Sprintf("%s (%d/%d)", constants.StatusPartial, running, total)
	default:
		return constants.StatusStopped
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
	output, err := RunQuiet("inspect", "-f", constants.InspectRunningFormat, name)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == constants.TrueString
}

// Pull pulls a docker image.
func Pull(image string) error {
	return Run("pull", image)
}

// ErrServiceNotRunning indicates a compose service is not currently running.
var ErrServiceNotRunning = fmt.Errorf("service not running")

// ConnectServiceToNetwork connects a docker compose service's containers to a network.
// It gets the container name(s) for the service and connects them to the specified network.
// An alias is added so the service can be reached by a predictable name.
// Returns ErrServiceNotRunning if the service container doesn't exist (e.g., uses profiles).
func ConnectServiceToNetwork(dir, serviceName, networkName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), StatusTimeout)
	defer cancel()

	// Get container ID for the service
	cmd := exec.CommandContext(ctx, "docker", "compose", "ps", "-q", serviceName)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return ErrServiceNotRunning
	}

	containerID := strings.TrimSpace(string(output))
	if containerID == "" {
		return ErrServiceNotRunning
	}

	// Connect to network with an alias matching the service name
	// This allows routing via http://{serviceName}:{port}
	connectCmd := exec.CommandContext(ctx, "docker", "network", "connect", "--alias", serviceName, networkName, containerID)
	if err := connectCmd.Run(); err != nil {
		// Check if already connected - try without alias
		errStr := err.Error()
		if strings.Contains(errStr, constants.ErrAlreadyExists) || strings.Contains(errStr, constants.ErrEndpointExists) {
			return nil
		}
		return fmt.Errorf("failed to connect to network: %w", err)
	}

	return nil
}

// ContainerExists checks if a container with the given name exists (running or stopped).
func ContainerExists(name string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), StatusTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "inspect", name)
	return cmd.Run() == nil
}

// GetContainerImageVersion returns the image version/tag for a running container.
// Returns an empty string if the container is not running or image info cannot be retrieved.
func GetContainerImageVersion(containerName string) string {
	ctx, cancel := context.WithTimeout(context.Background(), StatusTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.Config.Image}}", containerName)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	image := strings.TrimSpace(string(output))
	// Extract version from image name (e.g., "traefik:v3.0" -> "v3.0", "traefik:latest" -> "latest")
	if idx := strings.LastIndex(image, ":"); idx != -1 {
		return image[idx+1:]
	}
	return "latest" // Default if no tag specified
}

// ConnectContainerToNetwork connects an existing container to a network with an optional alias.
func ConnectContainerToNetwork(containerName, networkName, alias string) error {
	ctx, cancel := context.WithTimeout(context.Background(), StatusTimeout)
	defer cancel()

	args := []string{"network", "connect"}
	if alias != "" {
		args = append(args, "--alias", alias)
	}
	args = append(args, networkName, containerName)

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		errStr := string(output)
		// Already connected is not an error
		if strings.Contains(errStr, constants.ErrAlreadyExists) || strings.Contains(errStr, constants.ErrEndpointExists) {
			return nil
		}
		return fmt.Errorf("failed to connect container to network: %s", errStr)
	}
	return nil
}
