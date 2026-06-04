// Package docker provides Docker and Docker Compose operations.
package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/platform"
)

// notRunningErr renders the "docker is not running" message with a platform-
// appropriate hint. macOS has no `systemctl`, so we point at Docker Desktop /
// `colima start` instead.
func notRunningErr() error {
	if platform.IsDarwin() {
		return fmt.Errorf("docker is not running or not installed.\n  Start Docker Desktop, or run `colima start` if you're on Colima")
	}
	return fmt.Errorf("docker is not running or not installed.\n  Start Docker Desktop or run: sudo systemctl start docker")
}

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

// sdkClient is the subset of the Docker SDK that srv actually calls. Wrapping
// the concrete *dockerclient.Client behind this interface lets tests substitute
// a fake without standing up a real daemon.
type sdkClient interface {
	Ping(ctx context.Context) (types.Ping, error)
	NetworkList(ctx context.Context, opts network.ListOptions) ([]network.Summary, error)
	NetworkCreate(ctx context.Context, name string, opts network.CreateOptions) (network.CreateResponse, error)
	NetworkRemove(ctx context.Context, name string) error
	NetworkConnect(ctx context.Context, networkID, containerID string, cfg *network.EndpointSettings) error
	ContainerInspect(ctx context.Context, name string) (container.InspectResponse, error)
	ContainerList(ctx context.Context, opts container.ListOptions) ([]container.Summary, error)
	ImagePull(ctx context.Context, ref string, opts image.PullOptions) (io.ReadCloser, error)
	Close() error
}

// newClientFn produces an sdkClient. Tests swap this to install a fake. By
// default it dials the daemon described by the standard Docker env vars.
var newClientFn = func() (sdkClient, error) {
	return dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
}

// SwapNewClient replaces the SDK client factory and returns a function that
// restores the previous one. Intended for use with t.Cleanup in tests.
func SwapNewClient(factory func() (sdkClient, error)) func() {
	prev := newClientFn
	newClientFn = factory
	return func() { newClientFn = prev }
}

// SwapNewClientErr replaces the SDK client factory with one that always
// returns the given error. Convenience helper for tests outside this package
// that cannot reach the unexported sdkClient type directly.
func SwapNewClientErr(err error) func() {
	return SwapNewClient(func() (sdkClient, error) { return nil, err })
}

// SwapNewClientOK replaces the SDK client factory with a no-op fake that
// satisfies Ping and Close but errors on every other call. Use for tests that
// only need EnsureRunning to succeed.
func SwapNewClientOK() func() {
	return SwapNewClient(func() (sdkClient, error) { return noopSDK{}, nil })
}

// SwapNewClientWithNetwork returns a factory whose NetworkList reports the
// given network as existing. Convenience helper for tests that need both
// EnsureRunning and EnsureInitialized to pass.
func SwapNewClientWithNetwork(name string) func() {
	return SwapNewClient(func() (sdkClient, error) {
		return networkFakeSDK{noopSDK: noopSDK{}, networkName: name}, nil
	})
}

// networkFakeSDK is a noopSDK that reports one network as existing.
type networkFakeSDK struct {
	noopSDK
	networkName string
}

func (f networkFakeSDK) NetworkList(ctx context.Context, opts network.ListOptions) ([]network.Summary, error) {
	return []network.Summary{{Name: f.networkName}}, nil
}

// newClient returns a Docker client using the environment-configured socket.
// The caller is responsible for closing it.
func newClient() (sdkClient, error) {
	return newClientFn()
}

// EnsureRunning checks that Docker is available and running.
func EnsureRunning() error {
	ctx, cancel := context.WithTimeout(context.Background(), InfoTimeout)
	defer cancel()

	cli, err := newClient()
	if err != nil {
		return notRunningErr()
	}
	defer func() { _ = cli.Close() }()

	if _, err := cli.Ping(ctx); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("docker check timed out. Try: docker info\n  Docker may be unresponsive or overloaded")
		}
		return notRunningErr()
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
		// Network already exists → idempotent no-op. errdefs.IsConflict
		// covers the HTTP 409 the daemon returns regardless of error wording.
		if cerrdefs.IsConflict(err) {
			return nil
		}
		return err
	}
	return nil
}

// ComposeUp runs docker compose up -d in the specified directory.
//
// NOTE: srv groups every stack (each site + the metrics stack) under one shared
// compose project ("srv"), so --remove-orphans MUST NOT be passed here — docker
// would treat every other stack's containers as orphans of the current compose
// file and delete them, so starting one site would wipe the metrics stack and
// the other sites. (Only the traefik/dns stack has its own project.) Per-stack
// orphan cleanup is given up in exchange for not nuking sibling stacks.
func ComposeUp(dir string) error {
	return ComposeUpWithProfile(dir, "")
}

// ComposeUpBuild runs docker compose up -d --build, forcing a rebuild of any
// images defined by a Dockerfile before starting the containers.
func ComposeUpBuild(dir string) error {
	return ComposeUpBuildWithProfile(dir, "")
}

// ComposeUpWithProfile runs docker compose up -d with a specific profile.
// See ComposeUp for why --remove-orphans is deliberately omitted.
func ComposeUpWithProfile(dir, profile string) error {
	args := []string{"up", "-d"}
	if profile != "" {
		return Compose(dir, append([]string{"--profile", profile}, args...)...)
	}
	return Compose(dir, args...)
}

// ComposeUpBuildWithProfile runs docker compose up -d --build with a specific profile.
func ComposeUpBuildWithProfile(dir, profile string) error {
	args := []string{"up", "-d", "--build"}
	if profile != "" {
		return Compose(dir, append([]string{"--profile", profile}, args...)...)
	}
	return Compose(dir, args...)
}

// ComposeDown runs docker compose down in the specified directory. It does NOT
// pass --remove-orphans: under the shared "srv" compose project that would tear
// down every other stack's containers (other sites + metrics), not just this
// one's. Down already removes the containers/networks defined in this dir's
// compose file, which is the intended scope.
func ComposeDown(dir string) error {
	return Compose(dir, "down")
}

// composePrefixedExec is the swappable seam for ComposePrefixed.
var composePrefixedExec = defaultComposePrefixedExec

func defaultComposePrefixedExec(dir, prefix string, args ...string) error {
	cmd := exec.Command("docker", append([]string{"compose"}, args...)...)
	cmd.Dir = dir
	cmd.Stdout = newPrefixWriter(os.Stdout, prefix)
	cmd.Stderr = newPrefixWriter(os.Stderr, prefix)
	return cmd.Run()
}

// SwapComposePrefixedExec replaces the ComposePrefixed implementation.
func SwapComposePrefixedExec(fn func(dir, prefix string, args ...string) error) func() {
	prev := composePrefixedExec
	composePrefixedExec = fn
	return func() { composePrefixedExec = prev }
}

// ComposePrefixed runs `docker compose <args...>` in dir and pipes stdout +
// stderr through a writer that prefixes every line with `[prefix] `. Used by
// `srv logs --all` to multiplex many sites into one terminal.
func ComposePrefixed(dir, prefix string, args ...string) error {
	return composePrefixedExec(dir, prefix, args...)
}

// prefixWriter prefixes every newline-terminated chunk it sees with "[name] ".
// Partial lines are buffered until the terminating \n arrives so each prefix
// lands at the start of a real line.
type prefixWriter struct {
	w      io.Writer
	prefix []byte
	buf    []byte
}

func newPrefixWriter(w io.Writer, prefix string) *prefixWriter {
	return &prefixWriter{w: w, prefix: []byte("[" + prefix + "] ")}
}

func (p *prefixWriter) Write(b []byte) (int, error) {
	n := len(b)
	p.buf = append(p.buf, b...)
	for {
		idx := indexByte(p.buf, '\n')
		if idx < 0 {
			return n, nil
		}
		line := append([]byte{}, p.prefix...)
		line = append(line, p.buf[:idx+1]...)
		if _, err := p.w.Write(line); err != nil {
			return n, err
		}
		p.buf = p.buf[idx+1:]
	}
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

// ComposeStop runs docker compose stop in the specified directory.
func ComposeStop(dir string) error {
	return Compose(dir, "stop")
}

// ComposeRestart runs docker compose restart in the specified directory.
func ComposeRestart(dir string) error {
	return Compose(dir, "restart")
}

// dockerExec is the swappable seam for Exec / ExecNonInteractive[At]. mode
// "interactive" attaches stdin; mode "stream" only attaches stdout/stderr.
var dockerExec = defaultDockerExec

func defaultDockerExec(interactive bool, args ...string) error {
	cmd := exec.Command("docker", args...)
	if interactive {
		cmd.Stdin = os.Stdin
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// SwapDockerExec replaces the docker exec invoker. Returns a restore func.
func SwapDockerExec(fn func(interactive bool, args ...string) error) func() {
	prev := dockerExec
	dockerExec = fn
	return func() { dockerExec = prev }
}

// Exec runs a command inside a running container with stdin/stdout/stderr attached.
// This is equivalent to `docker exec -it <container> <args...>`.
func Exec(container string, args ...string) error {
	return dockerExec(true, append([]string{"exec", "-it", container}, args...)...)
}

// ExecNonInteractive runs a command inside a container without a TTY,
// streaming its output to stdout/stderr. Use this for automated steps where
// there is no terminal attached (e.g. running composer install after srv add).
func ExecNonInteractive(container string, args ...string) error {
	return ExecNonInteractiveAt(container, "", args...)
}

// ExecNonInteractiveAt is like ExecNonInteractive but runs the command with
// the supplied working directory (`-w`) inside the container. Empty workDir
// uses the image's default WORKDIR.
func ExecNonInteractiveAt(container, workDir string, args ...string) error {
	full := []string{"exec"}
	if workDir != "" {
		full = append(full, "-w", workDir)
	}
	full = append(full, container)
	full = append(full, args...)
	return dockerExec(false, full...)
}

// composeExec is the seam tests use to intercept `docker compose` invocations.
// quiet=true means stdout/stderr are not attached (mirroring ComposeQuiet).
var composeExec = defaultComposeExec

func defaultComposeExec(dir string, quiet bool, args ...string) error {
	if quiet {
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
	cmd := exec.Command("docker", append([]string{"compose"}, args...)...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// SwapComposeExec replaces the compose subprocess invoker. Returns a restore
// func suitable for t.Cleanup. Use this to assert on the args a compose call
// was made with.
func SwapComposeExec(fn func(dir string, quiet bool, args ...string) error) func() {
	prev := composeExec
	composeExec = fn
	return func() { composeExec = prev }
}

// Compose runs docker compose with given arguments in the specified directory.
// Output is attached to stdout/stderr for interactive use.
// docker compose is intentionally kept as a shell-out: the Docker SDK has no
// compose support; compose-go can parse manifests but cannot orchestrate them.
func Compose(dir string, args ...string) error {
	return composeExec(dir, false, args...)
}

// ComposeQuiet runs docker compose without stdout/stderr (for parallel execution).
func ComposeQuiet(dir string, args ...string) error {
	return composeExec(dir, true, args...)
}

// ComposeQuietWithProfile runs docker compose with a profile without stdout/stderr.
func ComposeQuietWithProfile(dir, profile string, args ...string) error {
	if profile == "" {
		return ComposeQuiet(dir, args...)
	}
	return ComposeQuiet(dir, append([]string{"--profile", profile}, args...)...)
}

// composePSOutput is the seam tests override to provide canned `docker compose
// ps` output without spawning a subprocess.
var composePSOutput = defaultComposePSOutput

func defaultComposePSOutput(dir string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), StatusTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "compose", "ps", "--format", constants.ComposeStatusFormat)
	cmd.Dir = dir
	return cmd.Output()
}

// SwapComposePSOutput replaces the compose ps output provider used by
// ContainerStatus. Returns a restore func for t.Cleanup.
func SwapComposePSOutput(fn func(dir string) ([]byte, error)) func() {
	prev := composePSOutput
	composePSOutput = fn
	return func() { composePSOutput = prev }
}

// ContainerStatus returns the status of containers in a compose project directory.
// Returns "running", "stopped", or "partial (n/m)".
func ContainerStatus(dir string) string {
	output, err := composePSOutput(dir)
	if err != nil {
		return constants.StatusStopped
	}
	return parseComposeStatusOutput(string(output))
}

// parseComposeStatusOutput aggregates the per-line `docker compose ps` output
// into a single status string. Each non-empty line is one container; lines
// starting with the Up prefix count as running.
func parseComposeStatusOutput(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
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
	return aggregateStatus(running, total)
}

// aggregateStatus turns (running, total) into the externally-visible status
// string. Extracted so SDK-driven callers (ContainerStatusByComposeDir) can
// share the same labelling logic.
func aggregateStatus(running, total int) string {
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
	return aggregateStatus(running, total)
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
var ErrServiceNotRunning = errors.New("service not running")

// composeServiceIDLookup is the seam that resolves a compose service to its
// container ID. Tests override it to skip the docker subprocess.
var composeServiceIDLookup = defaultComposeServiceIDLookup

func defaultComposeServiceIDLookup(ctx context.Context, dir, serviceName string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "compose", "ps", "-q", serviceName)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// SwapComposeServiceIDLookup replaces the compose service ID resolver. Returns
// a restore func suitable for t.Cleanup.
func SwapComposeServiceIDLookup(fn func(ctx context.Context, dir, serviceName string) (string, error)) func() {
	prev := composeServiceIDLookup
	composeServiceIDLookup = fn
	return func() { composeServiceIDLookup = prev }
}

// ConnectServiceToNetwork connects a docker compose service's container(s) to a
// network with a named alias so Traefik can route to the service by name.
// Returns ErrServiceNotRunning if the service container is not found.
func ConnectServiceToNetwork(dir, serviceName, networkName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), StatusTimeout)
	defer cancel()

	containerID, err := composeServiceIDLookup(ctx, dir, serviceName)
	if err != nil {
		return ErrServiceNotRunning
	}
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

	return extractImageTag(info.Config.Image)
}

// extractImageTag returns the tag portion of "image:tag" or "latest" when
// untagged. Empty input yields "latest" to mirror Docker's default tag.
func extractImageTag(image string) string {
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
		// Container is already attached to the network → idempotent no-op.
		// Docker returns HTTP 409 for both "already exists" and "endpoint with
		// name <x> already exists" — errdefs.IsConflict catches both.
		if cerrdefs.IsConflict(err) {
			return nil
		}
		return fmt.Errorf("failed to connect container to network: %w", err)
	}
	return nil
}

// noopSDK satisfies sdkClient with permissive defaults for tests. Ping
// succeeds; Close succeeds; everything else returns an "unimplemented" error
// so callers that look beyond reachability see a controlled failure.
type noopSDK struct{}

func (noopSDK) Ping(context.Context) (types.Ping, error) { return types.Ping{}, nil }
func (noopSDK) NetworkList(context.Context, network.ListOptions) ([]network.Summary, error) {
	return nil, nil
}
func (noopSDK) NetworkCreate(context.Context, string, network.CreateOptions) (network.CreateResponse, error) {
	return network.CreateResponse{}, nil
}
func (noopSDK) NetworkRemove(context.Context, string) error { return nil }
func (noopSDK) NetworkConnect(context.Context, string, string, *network.EndpointSettings) error {
	return nil
}
func (noopSDK) ContainerInspect(context.Context, string) (container.InspectResponse, error) {
	return container.InspectResponse{}, errors.New("noopSDK: not found")
}
func (noopSDK) ContainerList(context.Context, container.ListOptions) ([]container.Summary, error) {
	return nil, nil
}
func (noopSDK) ImagePull(context.Context, string, image.PullOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (noopSDK) Close() error { return nil }
