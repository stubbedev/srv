package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/shell"
	"github.com/stubbedev/srv/internal/shell/shelltest"
)

func envDockerHost() string { return os.Getenv(constants.EnvDockerHost) }

// clean gives a test a resolution with nothing inherited from the developer's
// own machine: no DOCKER_HOST, no override, and a fresh cache either side.
func clean(t *testing.T) {
	t.Helper()
	t.Setenv(constants.EnvDockerHost, "")
	t.Setenv(constants.EnvContainerEngine, "")
	os.Unsetenv(constants.EnvDockerHost)
	os.Unsetenv(constants.EnvContainerEngine)
	ResetCache()
	t.Cleanup(ResetCache)
}

func TestDockerIsTheDefaultAndTheFallback(t *testing.T) {
	// Nothing configured, nothing detectable: srv must still behave as it did
	// before any of this existed, so the failure reads "docker is not running".
	e := For("")
	if e.Name != Docker || e.Binary != Docker {
		t.Fatalf("For(\"\") = %+v, want docker", e)
	}
	if For(Auto).Name != Docker {
		t.Errorf("For(auto) = %q, want docker", For(Auto).Name)
	}
}

func TestSocketMountKeepsContainerPathConstant(t *testing.T) {
	// Traefik's providers.docker endpoint is one value for every runtime, so
	// each runtime's socket has to land on the same path inside the container.
	for _, r := range runtimes {
		e := For(r.name)
		want := e.Socket() + ":" + ContainerSocketPath + ":ro"
		if got := e.SocketMount(); got != want {
			t.Errorf("%s mount = %q, want %q", r.name, got, want)
		}
		if got := e.TraefikEndpoint(); got != unixScheme+ContainerSocketPath {
			t.Errorf("%s endpoint = %q, want the fixed container path", r.name, got)
		}
	}
}

// A tcp:// daemon has no socket to bind, so Traefik must be pointed at the URL
// itself rather than handed an empty volume entry.
func TestRemoteEndpointHasNoMountAndPassesThrough(t *testing.T) {
	e := Engine{Name: Docker, Binary: Docker, Endpoint: "tcp://10.0.0.1:2375"}
	if got := e.Socket(); got != "" {
		t.Errorf("Socket() = %q, want empty for a tcp endpoint", got)
	}
	if got := e.SocketMount(); got != "" {
		t.Errorf("SocketMount() = %q, want empty for a tcp endpoint", got)
	}
	if got := e.TraefikEndpoint(); got != "tcp://10.0.0.1:2375" {
		t.Errorf("TraefikEndpoint() = %q, want the url passed through", got)
	}
}

func TestRootlessPodmanSocketFollowsXDGRuntimeDir(t *testing.T) {
	t.Setenv(constants.EnvXDGRuntimeDir, "/run/user/1000")
	if os.Geteuid() == 0 {
		t.Skip("running as root — the per-user socket does not apply")
	}
	want := filepath.Join("/run/user/1000", "podman", "podman.sock")
	if got := podmanSockets()[0]; got != want {
		t.Errorf("first podman candidate = %q, want %q", got, want)
	}
	if got := dockerSockets()[0]; got != "/var/run/docker.sock" {
		t.Errorf("first docker candidate = %q, want the daemon socket", got)
	}
}

func TestValidateRejectsUnknownRuntimes(t *testing.T) {
	for _, name := range append([]string{""}, Supported...) {
		if err := Validate(name); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", name, err)
		}
	}
	if err := Validate("podmna"); err == nil {
		t.Error("Validate(\"podmna\") = nil, want an error naming the supported set")
	}
	// nerdctl and finch are plausible guesses with a specific reason for being
	// unsupported; the error has to say what that reason is.
	err := Validate("nerdctl")
	if err == nil {
		t.Fatal("Validate(\"nerdctl\") = nil, want an error")
	}
	if !strings.Contains(err.Error(), "Docker-compatible API") {
		t.Errorf("nerdctl error = %q, want it to explain the missing API", err)
	}
}

// Detection is what removes the need to configure anything: a machine with the
// runtime's socket present and its CLI installed resolves to that runtime.
func TestDetectPicksTheRuntimeWhoseSocketExists(t *testing.T) {
	clean(t)
	// Point podman's candidate list at a socket that really exists, and make
	// docker's not exist, by giving XDG_RUNTIME_DIR a temp dir we populate.
	dir := t.TempDir()
	t.Setenv(constants.EnvXDGRuntimeDir, dir)
	if os.Geteuid() == 0 {
		t.Skip("running as root — podman's per-user socket does not apply")
	}
	sock := filepath.Join(dir, "podman", "podman.sock")
	if err := os.MkdirAll(filepath.Dir(sock), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// Only podman is "installed" — so even on a machine with a real
	// /var/run/docker.sock, docker fails the binary half of the probe.
	t.Cleanup(shell.SwapDefault(shelltest.New(map[string]shelltest.Response{
		"podman": {Exists: true},
	})))

	e, ok := Detect()
	if !ok {
		t.Fatal("Detect() found nothing, want podman")
	}
	if e.Name != Podman {
		t.Fatalf("Detect() = %q, want podman", e.Name)
	}
	if !e.Detected() {
		t.Error("Detected() = false, want true so doctor can say how it was chosen")
	}
	if e.Endpoint != unixScheme+sock {
		t.Errorf("endpoint = %q, want %q", e.Endpoint, unixScheme+sock)
	}
}

func TestDetectReportsNothingWhenNoRuntimeIsUsable(t *testing.T) {
	clean(t)
	t.Setenv(constants.EnvXDGRuntimeDir, t.TempDir())
	// No binary exists, so no candidate can win regardless of stray sockets.
	t.Cleanup(shell.SwapDefault(shelltest.New(nil)))
	if e, ok := Detect(); ok {
		t.Errorf("Detect() = %+v, want no match", e)
	}
}

func TestExplicitDockerHostWinsOverEverything(t *testing.T) {
	clean(t)
	t.Setenv(constants.EnvContainerEngine, Podman)
	t.Setenv(constants.EnvDockerHost, "tcp://10.0.0.1:2375")

	e := Current()
	if e.Endpoint != "tcp://10.0.0.1:2375" {
		t.Errorf("endpoint = %q, want the operator's DOCKER_HOST untouched", e.Endpoint)
	}
	if envDockerHost() != "tcp://10.0.0.1:2375" {
		t.Errorf("DOCKER_HOST = %q, want it left alone", envDockerHost())
	}
}

// A DOCKER_HOST that happens to be a known runtime's socket still resolves to
// that runtime, so `podman compose` is used rather than `docker compose`.
func TestExplicitDockerHostIsNamedWhenItIsAKnownSocket(t *testing.T) {
	clean(t)
	t.Setenv(constants.EnvDockerHost, unixScheme+"/run/podman/podman.sock")
	if got := Current(); got.Name != Podman || got.Binary != Podman {
		t.Errorf("Current() = %+v, want podman", got)
	}
}

func TestConfiguredRuntimeExportsItsEndpoint(t *testing.T) {
	clean(t)
	t.Setenv(constants.EnvContainerEngine, Podman)

	e := Current()
	if e.Name != Podman {
		t.Fatalf("engine = %q, want podman", e.Name)
	}
	if envDockerHost() != e.Endpoint {
		t.Errorf("DOCKER_HOST = %q, want %q", envDockerHost(), e.Endpoint)
	}
}

func TestUnknownEngineFallsThroughToDetection(t *testing.T) {
	clean(t)
	t.Setenv(constants.EnvContainerEngine, "podmna")
	t.Cleanup(shell.SwapDefault(shelltest.New(nil)))

	// Nothing detectable in the test env, so this lands on the docker fallback
	// rather than on a runtime named after the typo.
	if got := Current().Name; got != Docker && got != Podman {
		t.Errorf("engine = %q, want a real runtime, not the typo", got)
	}
	if _, err := Configured(); err == nil {
		t.Error("Configured() error = nil, want doctor to be able to report the typo")
	}
}

// The source has to be recorded at resolution time, not inferred afterwards:
// srv exports DOCKER_HOST itself, so anything that reads the variable back
// would report every run as "from DOCKER_HOST".
func TestSourceDistinguishesConfiguredFromInherited(t *testing.T) {
	clean(t)
	t.Setenv(constants.EnvContainerEngine, Podman)
	if got := Current().Source; got != SourceConfig {
		t.Errorf("source = %q, want %q", got, SourceConfig)
	}
	if envDockerHost() == "" {
		t.Error("DOCKER_HOST was not exported, so the SDK client would miss the runtime")
	}

	ResetCache()
	t.Setenv(constants.EnvContainerEngine, "")
	os.Unsetenv(constants.EnvContainerEngine)
	t.Setenv(constants.EnvDockerHost, "tcp://10.0.0.1:2375")
	if got := Current().Source; got != SourceEnv {
		t.Errorf("source = %q, want %q", got, SourceEnv)
	}
}
