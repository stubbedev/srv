package ops

import (
	"os"
	"testing"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/engine"
	"github.com/stubbedev/srv/internal/shell"
	"github.com/stubbedev/srv/internal/shell/shelltest"
)

func envDockerHost() string { return os.Getenv(constants.EnvDockerHost) }

// clean gives a resolution nothing inherited from the developer's own machine:
// no DOCKER_HOST, no override, a fresh SRV_ROOT and a fresh cache either side.
func clean(t *testing.T) {
	t.Helper()
	withRoot(t)
	t.Setenv(constants.EnvDockerHost, "")
	os.Unsetenv(constants.EnvDockerHost)
	ResetEngineCache()
	t.Cleanup(ResetEngineCache)
}

func TestExplicitDockerHostWinsOverEverything(t *testing.T) {
	clean(t)
	t.Setenv(constants.EnvContainerEngine, engine.Podman)
	t.Setenv(constants.EnvDockerHost, "tcp://10.0.0.1:2375")

	e := Engine()
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
	t.Setenv(constants.EnvDockerHost, "unix://"+"/run/podman/podman.sock")
	if got := Engine(); got.Name != engine.Podman || got.Binary != engine.Podman {
		t.Errorf("Engine() = %+v, want podman", got)
	}
}

func TestConfiguredRuntimeExportsItsEndpoint(t *testing.T) {
	clean(t)
	t.Setenv(constants.EnvContainerEngine, engine.Podman)

	e := Engine()
	if e.Name != engine.Podman {
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
	if got := Engine().Name; got != engine.Docker && got != engine.Podman {
		t.Errorf("engine = %q, want a real runtime, not the typo", got)
	}
	if _, err := ConfiguredEngine(); err == nil {
		t.Error("ConfiguredEngine() error = nil, want doctor to be able to report the typo")
	}
}

// The source has to be recorded at resolution time, not inferred afterwards:
// srv exports DOCKER_HOST itself, so anything that reads the variable back
// would report every run as "from DOCKER_HOST".
func TestSourceDistinguishesConfiguredFromInherited(t *testing.T) {
	clean(t)
	t.Setenv(constants.EnvContainerEngine, engine.Podman)
	if got := Engine().Source; got != engine.SourceConfig {
		t.Errorf("source = %q, want %q", got, engine.SourceConfig)
	}
	if envDockerHost() == "" {
		t.Error("DOCKER_HOST was not exported, so the SDK client would miss the runtime")
	}

	ResetEngineCache()
	t.Setenv(constants.EnvContainerEngine, "")
	os.Unsetenv(constants.EnvContainerEngine)
	t.Setenv(constants.EnvDockerHost, "tcp://10.0.0.1:2375")
	if got := Engine().Source; got != engine.SourceEnv {
		t.Errorf("source = %q, want %q", got, engine.SourceEnv)
	}
}
