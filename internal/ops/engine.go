package ops

import (
	"os"
	"sync"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/engine"
)

// Engine resolution lives here rather than in internal/engine because it is a
// *configuration* question — which runtime has the user asked for, or which
// one is actually present — and that answer has to come from the same
// validated view of config.yml as everything else. internal/engine stays a
// pure table of runtimes and probes with no idea a config file exists, which
// is also what keeps it free of an import cycle with this package.

var (
	engineMu     sync.Mutex
	engineLoaded bool
	engineCur    engine.Engine
)

// Engine returns the resolved container runtime, resolving on first use and
// exporting DOCKER_HOST as a side effect so the Docker SDK client
// (dockerclient.FromEnv) and Compose v2 both follow the same choice.
func Engine() engine.Engine {
	engineMu.Lock()
	defer engineMu.Unlock()
	if !engineLoaded {
		engineCur = resolveEngine()
		engineLoaded = true
	}
	return engineCur
}

// EngineName is shorthand for Engine().Name.
func EngineName() string { return Engine().Name }

// EngineBinary is shorthand for Engine().Binary.
func EngineBinary() string { return Engine().Binary }

// ComposeArgs is shorthand for Engine().ComposeArgs.
func ComposeArgs(args ...string) []string { return Engine().ComposeArgs(args...) }

// SwapEngine replaces the resolved runtime and returns a restore func for
// t.Cleanup.
func SwapEngine(e engine.Engine) func() {
	engineMu.Lock()
	defer engineMu.Unlock()
	prev, prevLoaded := engineCur, engineLoaded
	engineCur, engineLoaded = e, true
	return func() {
		engineMu.Lock()
		defer engineMu.Unlock()
		engineCur, engineLoaded = prev, prevLoaded
	}
}

// ResetEngineCache forces the next Engine() to resolve again.
func ResetEngineCache() {
	engineMu.Lock()
	defer engineMu.Unlock()
	engineLoaded = false
	engineCur = engine.Engine{}
}

// ConfiguredEngine returns the raw container_engine value the user wrote (from
// the environment override or config.yml) together with its validation error,
// so `srv doctor` can report a typo rather than silently falling back.
func ConfiguredEngine() (string, error) {
	name := rawEngineName()
	return name, engine.Validate(name)
}

// resolveEngine applies the precedence: an explicit DOCKER_HOST, then the
// configured runtime, then detection, then Docker.
func resolveEngine() engine.Engine {
	// An operator who exported DOCKER_HOST has named the endpoint directly, and
	// it is the more specific instruction than any name — it is also the only
	// way to reach a runtime that is not in the table (a remote daemon, a shim).
	if host := os.Getenv(constants.EnvDockerHost); host != "" {
		return engine.ForEndpoint(host)
	}

	if name := configuredEngineName(); name != "" && name != engine.Auto {
		e := engine.For(name)
		e.Source = engine.SourceConfig
		return exportEngine(e)
	}

	if e, ok := engine.Detect(); ok {
		return exportEngine(e)
	}

	e := engine.For(engine.Docker)
	e.Source = engine.SourceFallback
	return exportEngine(e)
}

// exportEngine publishes the endpoint as DOCKER_HOST so the SDK client and
// Compose v2 follow the resolved runtime.
func exportEngine(e engine.Engine) engine.Engine {
	_ = os.Setenv(constants.EnvDockerHost, e.Endpoint)
	return e
}

// configuredEngineName returns the validated runtime name, empty for anything
// unreadable or unknown — a broken key falls through to detection rather than
// making every command fail.
func configuredEngineName() string {
	name := rawEngineName()
	if engine.Validate(name) != nil {
		return ""
	}
	return name
}

// rawEngineName returns the container_engine value as written, empty when
// neither the environment nor config.yml says anything.
func rawEngineName() string {
	if name := os.Getenv(constants.EnvContainerEngine); name != "" {
		return name
	}
	// Read past validation: reporting the bad value is ConfiguredEngine's job,
	// and Engine() has to keep working meanwhile.
	cfg, err := UserConfig()
	if cfg == nil || (err != nil && cfg.ContainerEngine == "") {
		return ""
	}
	return cfg.ContainerEngine
}
