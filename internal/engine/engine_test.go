package engine

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/shell"
	"github.com/stubbedev/srv/internal/shell/shelltest"
)

// clean gives a probe nothing inherited from the developer's own machine.
func clean(t *testing.T) {
	t.Helper()
	t.Setenv(constants.EnvDockerHost, "")
	t.Setenv(constants.EnvContainerEngine, "")
	os.Unsetenv(constants.EnvDockerHost)
	os.Unsetenv(constants.EnvContainerEngine)
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
	// A real listener, not just a file: the probe dials, because a socket file
	// outlives the daemon that made it.
	ln, lnErr := net.Listen("unix", sock)
	if lnErr != nil {
		t.Skipf("cannot bind a unix socket here: %v", lnErr)
	}
	defer ln.Close()
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

// ─── socket probing ──────────────────────────────────────────────────────
//
// This is what CI caught: the GitHub runner had a podman socket *file* at the
// rootless path with nothing behind it, and the live rootful socket further
// down the candidate list. Existence-based probing picked the dead one and
// every compose call failed with "Cannot connect to the Docker daemon".

func TestFirstUsableSkipsAStaleSocketFile(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "stale.sock")
	if err := os.WriteFile(stale, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	live := filepath.Join(dir, "live.sock")
	ln, err := net.Listen("unix", live)
	if err != nil {
		t.Skipf("cannot bind a unix socket here: %v", err)
	}
	defer ln.Close()

	got, ok := firstUsable([]string{stale, live})
	if !ok {
		t.Fatal("firstUsable() found nothing, want the live socket")
	}
	if got != live {
		t.Errorf("firstUsable() = %q, want the live socket %q — a stale file must not win", got, live)
	}
}

func TestFirstUsableIgnoresMissingAndEmptyPaths(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "live.sock")
	ln, err := net.Listen("unix", live)
	if err != nil {
		t.Skipf("cannot bind a unix socket here: %v", err)
	}
	defer ln.Close()

	got, ok := firstUsable([]string{"", filepath.Join(dir, "nope.sock"), live})
	if !ok || got != live {
		t.Errorf("firstUsable() = %q, %v; want the live socket", got, ok)
	}
}

func TestFirstUsableReportsNothingWhenAllAreDead(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "stale.sock")
	if err := os.WriteFile(stale, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, ok := firstUsable([]string{stale, filepath.Join(dir, "absent.sock")}); ok {
		t.Errorf("firstUsable() = %q, want no match", got)
	}
}

// A regular file is not a socket; dialling it fails, which is the point.
func TestDetectIgnoresARuntimeWithOnlyAStaleSocket(t *testing.T) {
	clean(t)
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
	t.Cleanup(shell.SwapDefault(shelltest.New(map[string]shelltest.Response{
		"podman": {Exists: true},
	})))

	if e, ok := Detect(); ok && e.Name == Podman {
		t.Errorf("Detect() chose podman on a stale socket: %+v", e)
	}
}
