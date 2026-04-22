// Package docker provides Docker and Docker Compose operations.
package docker

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"

	"github.com/stubbedev/srv/internal/constants"
)

// Timeout constants for Docker operations.
const (
	// InfoTimeout is for quick daemon-availability checks.
	InfoTimeout = 10 * time.Second
	// StatusTimeout is for status/inspect operations.
	StatusTimeout = 30 * time.Second
	// ComposeTimeout is for compose operations (image pulls, builds, etc.).
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

// newClient returns a Docker client using the environment-configured socket.
// The caller is responsible for closing it.
func newClient() (*dockerclient.Client, error) {
	return dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
}

// EnsureRunning checks that Docker is available and running.
func EnsureRunning() error {
	ctx, cancel := context.WithTimeout(context.Background(), InfoTimeout)
	defer cancel()

	cli, err := newClient()
	if err != nil {
		return fmt.Errorf("docker is not running or not installed.\n  Start Docker Desktop or run: sudo systemctl start docker")
	}
	defer func() { _ = cli.Close() }()

	if _, err := cli.Ping(ctx); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("docker check timed out. Try: docker info\n  Docker may be unresponsive or overloaded")
		}
		return fmt.Errorf("docker is not running or not installed.\n  Start Docker Desktop or run: sudo systemctl start docker")
	}
	return nil
}

// EnsureInitialized checks that the srv proxy network exists, which is created
// by srv install. Returns a clear error directing the user to run srv install if not.
func EnsureInitialized(networkName string) error {
	if !NetworkExists(networkName) {
		return fmt.Errorf("srv is not installed. Run: srv install")
	}
	return nil
}

// NetworkExists checks if a Docker network with the given name exists.
func NetworkExists(name string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), StatusTimeout)
	defer cancel()

	cli, err := newClient()
	if err != nil {
		return false
	}
	defer func() { _ = cli.Close() }()

	f := filters.NewArgs(filters.Arg("name", name))
	networks, err := cli.NetworkList(ctx, network.ListOptions{Filters: f})
	if err != nil {
		return false
	}
	// NetworkList with a name filter does prefix matching; check for exact match.
	for _, n := range networks {
		if n.Name == name {
			return true
		}
	}
	return false
}

// CreateNetwork creates a Docker bridge network with the given name.
func CreateNetwork(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), StatusTimeout)
	defer cancel()

	cli, err := newClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer func() { _ = cli.Close() }()

	_, err = cli.NetworkCreate(ctx, name, network.CreateOptions{Driver: "bridge"})
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("docker network create timed out")
		}
		if strings.Contains(err.Error(), constants.ErrAlreadyExists) {
			return nil
		}
		return err
	}
	return nil
}

// ComposeUp runs docker compose up -d in the specified directory.
// Uses --remove-orphans to clean up stale containers.
func ComposeUp(dir string) error {
	return ComposeUpWithProfile(dir, "")
}

// ComposeUpBuild runs docker compose up -d --build, forcing a rebuild of any
// images defined by a Dockerfile before starting the containers.
func ComposeUpBuild(dir string) error {
	return ComposeUpBuildWithProfile(dir, "")
}

// ComposeUpWithProfile runs docker compose up -d with a specific profile.
func ComposeUpWithProfile(dir, profile string) error {
	args := []string{"up", "-d", "--remove-orphans"}
	if profile != "" {
		return Compose(dir, append([]string{"--profile", profile}, args...)...)
	}
	return Compose(dir, args...)
}

// ComposeUpBuildWithProfile runs docker compose up -d --build with a specific profile.
func ComposeUpBuildWithProfile(dir, profile string) error {
	args := []string{"up", "-d", "--build", "--remove-orphans"}
	if profile != "" {
		return Compose(dir, append([]string{"--profile", profile}, args...)...)
	}
	return Compose(dir, args...)
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

// Exec runs a command inside a running container with stdin/stdout/stderr attached.
// This is equivalent to `docker exec -it <container> <args...>`.
func Exec(container string, args ...string) error {
	cmd := exec.Command("docker", append([]string{"exec", "-it", container}, args...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ExecNonInteractive runs a command inside a container without a TTY,
// streaming its output to stdout/stderr. Use this for automated steps where
// there is no terminal attached (e.g. running composer install after srv add).
func ExecNonInteractive(container string, args ...string) error {
	cmd := exec.Command("docker", append([]string{"exec", container}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Compose runs docker compose with given arguments in the specified directory.
// Output is attached to stdout/stderr for interactive use.
// docker compose is intentionally kept as a shell-out: the Docker SDK has no
// compose support; compose-go can parse manifests but cannot orchestrate them.
func Compose(dir string, args ...string) error {
	cmd := exec.Command("docker", append([]string{"compose"}, args...)...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ComposeQuiet runs docker compose without stdout/stderr (for parallel execution).
func ComposeQuiet(dir string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), ComposeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose"}, args...)...)
	cmd.Dir = dir
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
	return ComposeQuiet(dir, append([]string{"--profile", profile}, args...)...)
}

// ContainerStatus returns the status of containers in a compose project directory.
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

// ContainerStatusByName returns the status of a single named container using
// the Docker SDK (no subprocess). Returns "running", "stopped", or "partial (n/m)".
// Falls back to ContainerStatus if the SDK call fails.
func ContainerStatusByName(containerName string) string {
	ctx, cancel := context.WithTimeout(context.Background(), StatusTimeout)
	defer cancel()

	cli, err := newClient()
	if err != nil {
		return constants.StatusStopped
	}
	defer func() { _ = cli.Close() }()

	info, err := cli.ContainerInspect(ctx, containerName)
	if err != nil {
		return constants.StatusStopped
	}
	if info.State != nil && info.State.Running {
		return constants.StatusRunning
	}
	return constants.StatusStopped
}

// ContainerStatusByComposeDir returns the aggregate status of all containers
// belonging to a Docker Compose project directory using the Docker SDK
// (no subprocess). Returns "running", "stopped", or "partial (n/m)".
// Falls back to ContainerStatus(dir) if the SDK call fails.
func ContainerStatusByComposeDir(dir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), StatusTimeout)
	defer cancel()

	cli, err := newClient()
	if err != nil {
		// Fall back to subprocess
		return ContainerStatus(dir)
	}
	defer func() { _ = cli.Close() }()

	f := filters.NewArgs(
		filters.Arg("label", "com.docker.compose.project.working_dir="+dir),
	)
	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true, Filters: f})
	if err != nil {
		// Fall back to subprocess
		return ContainerStatus(dir)
	}

	var running, total int
	for _, c := range containers {
		total++
		if c.State == "running" {
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

// IsContainerRunning checks if a container with the given name is currently running.
func IsContainerRunning(name string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), StatusTimeout)
	defer cancel()

	cli, err := newClient()
	if err != nil {
		return false
	}
	defer func() { _ = cli.Close() }()

	info, err := cli.ContainerInspect(ctx, name)
	if err != nil {
		return false
	}
	return info.State != nil && info.State.Running
}

// Pull pulls a Docker image, streaming progress to stdout.
func Pull(imageName string) error {
	cli, err := newClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer func() { _ = cli.Close() }()

	// ImagePull returns a reader that must be consumed to drive the transfer.
	// Copy it to stdout so the user sees progress, then discard cleanly.
	// Use ComposeTimeout so a stalled daemon or network issue doesn't hang forever.
	ctx, cancel := context.WithTimeout(context.Background(), ComposeTimeout)
	defer cancel()
	reader, err := cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image %s: %w", imageName, err)
	}
	defer func() { _ = reader.Close() }()

	_, err = io.Copy(os.Stdout, reader)
	return err
}

// ErrServiceNotRunning indicates a compose service is not currently running.
var ErrServiceNotRunning = fmt.Errorf("service not running")

// ConnectServiceToNetwork connects a docker compose service's container(s) to a
// network with a named alias so Traefik can route to the service by name.
// Returns ErrServiceNotRunning if the service container is not found.
func ConnectServiceToNetwork(dir, serviceName, networkName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), StatusTimeout)
	defer cancel()

	// Resolve the container ID via compose ps — no SDK equivalent for compose.
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

	return connectContainerByID(ctx, containerID, networkName, serviceName)
}

// ContainerExists checks if a container with the given name exists (running or stopped).
func ContainerExists(name string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), StatusTimeout)
	defer cancel()

	cli, err := newClient()
	if err != nil {
		return false
	}
	defer func() { _ = cli.Close() }()

	_, err = cli.ContainerInspect(ctx, name)
	return err == nil
}

// GetContainerImageVersion returns the image tag for a running container.
// Returns an empty string if the container is not found or the image has no tag.
func GetContainerImageVersion(containerName string) string {
	ctx, cancel := context.WithTimeout(context.Background(), StatusTimeout)
	defer cancel()

	cli, err := newClient()
	if err != nil {
		return ""
	}
	defer func() { _ = cli.Close() }()

	info, err := cli.ContainerInspect(ctx, containerName)
	if err != nil {
		return ""
	}

	image := info.Config.Image
	if idx := strings.LastIndex(image, ":"); idx != -1 {
		return image[idx+1:]
	}
	return "latest"
}

// RemoveNetwork removes a Docker network by name.
func RemoveNetwork(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), StatusTimeout)
	defer cancel()

	cli, err := newClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer func() { _ = cli.Close() }()

	return cli.NetworkRemove(ctx, name)
}

// ConnectContainerToNetwork connects an existing container to a network with an
// optional alias.
func ConnectContainerToNetwork(containerName, networkName, alias string) error {
	ctx, cancel := context.WithTimeout(context.Background(), StatusTimeout)
	defer cancel()

	return connectContainerByID(ctx, containerName, networkName, alias)
}

// connectContainerByID is the shared implementation for network connect calls.
func connectContainerByID(ctx context.Context, containerID, networkName, alias string) error {
	cli, err := newClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer func() { _ = cli.Close() }()

	endpointCfg := &network.EndpointSettings{}
	if alias != "" {
		endpointCfg.Aliases = []string{alias}
	}

	err = cli.NetworkConnect(ctx, networkName, containerID, endpointCfg)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, constants.ErrAlreadyExists) || strings.Contains(errStr, constants.ErrEndpointExists) {
			return nil
		}
		return fmt.Errorf("failed to connect container to network: %w", err)
	}
	return nil
}
