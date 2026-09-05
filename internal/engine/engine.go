// Package engine resolves which container runtime srv drives, and where its
// API lives.
//
// srv needs one thing from a runtime: a Docker-compatible API endpoint. The SDK
// client uses it to list and create networks, inspect containers and pull
// images, and Traefik uses it to watch container labels. Everything else srv
// does — compose up/down, exec into a service — is a CLI call that every
// runtime here spells the same way.
//
// That requirement, not the vendor's name, is what defines the supported set.
// Docker and Podman ship such an endpoint. So do the Docker distributions that
// people actually run on macOS — Colima, OrbStack, Rancher Desktop — which are
// the same `docker` CLI over a socket in a different place; those used to be
// unusable with srv unless you exported DOCKER_HOST by hand. nerdctl and Finch
// do not ship one at all (containerd's socket is not a Docker API), which is
// why they are absent: srv could not create a network or inspect a container
// through them. Anything else that does speak the API — a remote daemon, a
// shim — is reachable by setting DOCKER_HOST, which srv honours above all else.
//
// Nothing here abstracts container operations. It answers three questions that
// used to be answered by a literal:
//
//	Which binary do I exec?       Engine.Binary / Engine.ComposeArgs
//	Which endpoint is the API on? Engine.Endpoint
//	What do I give Traefik?       Engine.SocketMount / Engine.TraefikEndpoint
//
// The endpoint is exported into DOCKER_HOST at resolution time, so the SDK
// client (dockerclient.FromEnv) and Compose v2 both follow it without further
// plumbing.
package engine

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/shell"
)

// Source says how an Engine was chosen — the first thing to know when srv is
// talking to a daemon you did not expect.
type Source string

const (
	SourceEnv      Source = "from DOCKER_HOST"
	SourceConfig   Source = "configured"
	SourceDetected Source = "detected"
	SourceFallback Source = "default"
)

// Runtime names srv accepts in `container_engine`.
const (
	Auto                = "auto"
	Docker              = "docker"
	Podman              = "podman"
	Colima              = "colima"
	OrbStack            = "orbstack"
	RancherDesktop      = "rancher-desktop"
	unixScheme          = "unix://"
	dockerDefaultSocket = "/var/run/docker.sock"
)

// ContainerSocketPath is where a unix socket is mounted *inside* the Traefik
// container, whichever runtime it came from. Only the host side of the bind
// varies, which keeps providers.docker.endpoint constant for the socket case.
const ContainerSocketPath = "/var/run/docker.sock"

// socketProbeTimeout bounds the connect used to tell a live socket from a
// stale file. Detection runs on the first engine call of every command, so it
// has to stay imperceptible.
const socketProbeTimeout = 250 * time.Millisecond

// runtime is one entry in the table below: a CLI plus the places its
// Docker-compatible API socket is conventionally found, best candidate first.
type runtime struct {
	name    string
	binary  string
	sockets func() []string
}

// runtimes is the detection order. Docker is first so a machine with a plain
// Docker install resolves exactly as it did before detection existed; the
// Docker-compatible distributions follow, then Podman.
var runtimes = []runtime{
	{Docker, Docker, dockerSockets},
	{OrbStack, Docker, func() []string { return []string{home(".orbstack", "run", "docker.sock")} }},
	{Colima, Docker, colimaSockets},
	{RancherDesktop, Docker, func() []string { return []string{home(".rd", "docker.sock")} }},
	{Podman, Podman, podmanSockets},
}

// Supported lists the values `container_engine` accepts, for error messages and
// the config schema.
var Supported = func() []string {
	names := make([]string, 0, 1+len(runtimes))
	names = append(names, Auto)
	for _, r := range runtimes {
		names = append(names, r.name)
	}
	return names
}()

// Engine is a resolved runtime: the CLI to exec and the Docker-compatible API
// endpoint to talk to.
type Engine struct {
	Name     string // "docker", "podman", "colima", …
	Binary   string // CLI to exec; "docker" for every Docker-compatible distribution
	Endpoint string // DOCKER_HOST value: unix:///… or tcp://…
	Source   Source // how this runtime was chosen
}

// Detected reports whether the runtime was found by probing rather than named.
func (e Engine) Detected() bool { return e.Source == SourceDetected }

// Socket returns the host path of the endpoint's unix socket, empty when the
// endpoint is not a unix socket (a tcp:// remote daemon, say).
func (e Engine) Socket() string {
	sock, ok := strings.CutPrefix(e.Endpoint, unixScheme)
	if !ok {
		return ""
	}
	return sock
}

// SocketMount is the compose volume entry that hands Traefik the runtime's API
// socket, or "" when there is no socket to bind — Traefik then reaches the
// endpoint over the network instead, via TraefikEndpoint.
func (e Engine) SocketMount() string {
	sock := e.Socket()
	if sock == "" {
		return ""
	}
	return sock + ":" + ContainerSocketPath + ":ro"
}

// TraefikEndpoint is the value for providers.docker.endpoint. A unix socket is
// bind-mounted to a fixed path, so the container always sees the same one; any
// other endpoint is passed through as written.
func (e Engine) TraefikEndpoint() string {
	if e.Socket() != "" {
		return unixScheme + ContainerSocketPath
	}
	return e.Endpoint
}

// ComposeArgs prefixes args with the runtime's compose subcommand. srv requires
// Compose v2 (the Go binary) whatever the runtime: it writes the
// com.docker.compose.* labels srv filters containers by. `podman compose`
// delegates to it; `podman-compose` (the Python reimplementation) does not, and
// `srv doctor` reports that rather than letting the lookups silently return
// nothing.
func (e Engine) ComposeArgs(args ...string) []string {
	return append([]string{"compose"}, args...)
}

// Rootless reports whether the runtime runs without root. Only meaningful for
// Podman; a Docker-compatible daemon is root-owned regardless of who invokes
// the CLI.
func (e Engine) Rootless() bool {
	return e.Name == Podman && os.Geteuid() != 0
}

// String renders the engine for user-facing messages.
func (e Engine) String() string { return e.Name }

// Validate reports whether name is a runtime srv knows how to drive.
func Validate(name string) error {
	if name == "" || slices.Contains(Supported, name) {
		return nil
	}
	hint := ""
	if name == "nerdctl" || name == "finch" {
		hint = " — " + name + " exposes no Docker-compatible API socket, which srv needs for networks and image pulls; point DOCKER_HOST at a shim if you have one"
	}
	return fmt.Errorf("unknown container_engine %q (supported: %s)%s", name, strings.Join(Supported, ", "), hint)
}

// For builds the Engine for a named runtime, using the first of its candidate
// sockets that exists — or, when none do, its conventional default so error
// messages still name a concrete path. Exported for the e2e harness, which
// drives one runtime per matrix leg.
func For(name string) Engine {
	if name == "" || name == Auto {
		name = Docker
	}
	for _, r := range runtimes {
		if r.name != name {
			continue
		}
		candidates := r.sockets()
		sock := candidates[0]
		if found, ok := firstUsable(candidates); ok {
			sock = found
		}
		return Engine{Name: r.name, Binary: r.binary, Endpoint: unixScheme + sock}
	}
	return Engine{Name: name, Binary: name, Endpoint: unixScheme + dockerDefaultSocket}
}

// Detect probes the runtime table for the first one that is actually usable
// here: its CLI on $PATH and one of its sockets present. Returns ok=false when
// nothing matches, which is not an error — the caller falls back to Docker so
// the failure surfaces as the familiar "docker is not running".
func Detect() (Engine, bool) {
	for _, r := range runtimes {
		sock, ok := firstUsable(r.sockets())
		if !ok || !shell.Exists(r.binary) {
			continue
		}
		return Engine{Name: r.name, Binary: r.binary, Endpoint: unixScheme + sock, Source: SourceDetected}, true
	}
	return Engine{}, false
}

// resolve applies the precedence: an explicit DOCKER_HOST, then the configured
// runtime, then detection, then Docker.

// ForEndpoint builds an Engine around an endpoint an operator supplied
// directly, naming the runtime whose socket it is so messages and the compose
// binary are still right. An unrecognised endpoint is Docker's — that is what a
// remote daemon or a Docker-API shim answers as.
func ForEndpoint(host string) Engine {
	sock := strings.TrimPrefix(host, unixScheme)
	for _, r := range runtimes {
		if slices.Contains(r.sockets(), sock) {
			return Engine{Name: r.name, Binary: r.binary, Endpoint: host, Source: SourceEnv}
		}
	}
	return Engine{Name: Docker, Binary: Docker, Endpoint: host, Source: SourceEnv}
}

// Configured returns the raw container_engine value the user wrote (from the
// environment override or config.yml) together with its validation error, so
// `srv doctor` can report a typo instead of silently falling back.

// ---- socket candidates -----------------------------------------------------

func dockerSockets() []string {
	// Docker Desktop's per-user socket is the fallback for macOS installs that
	// no longer symlink the system path.
	return []string{dockerDefaultSocket, home(".docker", "run", "docker.sock")}
}

func colimaSockets() []string {
	// Colima namespaces its socket per profile; "default" is the one `colima
	// start` creates with no --profile.
	return []string{
		home(".colima", "default", "docker.sock"),
		home(".colima", "docker.sock"),
	}
}

func podmanSockets() []string {
	var paths []string
	if runtimeDir := os.Getenv(constants.EnvXDGRuntimeDir); runtimeDir != "" && os.Geteuid() != 0 {
		paths = append(paths, filepath.Join(runtimeDir, "podman", "podman.sock"))
	}
	return append(paths, "/run/podman/podman.sock")
}

// home joins parts onto the user's home directory, returning a path that cannot
// exist when the home directory is unknown so probing simply skips it.
func home(parts ...string) string {
	dir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(append([]string{dir}, parts...)...)
}

// firstUsable returns the first socket in paths that something is actually
// listening on.
//
// Existence is not enough. A socket file outlives the daemon that made it —
// a stopped Colima or OrbStack VM, a systemd user unit that was enabled and
// then failed to start — and the stale file sorts ahead of a live socket
// further down the list, so srv would resolve an endpoint that refuses every
// connection. Dialling is what tells the two apart, and against a local unix
// socket it costs microseconds.
func firstUsable(paths []string) (string, bool) {
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err != nil {
			continue
		}
		// Short: a local socket either answers immediately or is not there.
		dialer := &net.Dialer{Timeout: socketProbeTimeout}
		conn, err := dialer.DialContext(context.Background(), "unix", p)
		if err != nil {
			continue
		}
		_ = conn.Close()
		return p, true
	}
	return "", false
}
